package test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"cpa-usage-keeper/internal/ranking"
)

func TestServiceDistinguishesCenterBanAndPersistsItAcrossRestart(t *testing.T) {
	for _, banned := range []bool{false, true} {
		name := "deleted"
		if banned {
			name = "banned"
		}
		t.Run(name, func(t *testing.T) {
			db := openRankingDatabase(t)
			store := ranking.NewStore(db)
			seedActiveState(t, store, 0)
			now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
			center := &centerStub{self: func(_ context.Context, credentials ranking.Credentials, _ time.Time) (ranking.SelfStatus, error) {
				remote := ranking.SelfStatus{ParticipantID: credentials.ParticipantID, DisplayName: "Keeper_01", AvatarID: 7, Status: "deleted", Banned: banned, DeletedAt: &now}
				return remote, nil
			}}
			service, err := ranking.NewService(store, &aggregatorStub{latestID: 1}, center)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.SyncNow(context.Background()); !errors.Is(err, ranking.ErrParticipantDeleted) {
				t.Fatalf("SyncNow error = %v", err)
			}
			restarted, err := ranking.NewService(ranking.NewStore(db), &aggregatorStub{latestID: 1}, center)
			if err != nil {
				t.Fatal(err)
			}
			status, err := restarted.Status(context.Background())
			if err != nil || status.Status != ranking.StatusDeleted || status.Banned != banned {
				t.Fatalf("restarted status = %s, banned = %t, error = %v", status.Status, status.Banned, err)
			}
			beforeSelf, beforeReports := center.selfCalls, center.reportCalls
			if err := restarted.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if center.selfCalls != beforeSelf || center.reportCalls != beforeReports {
				t.Fatal("terminal state continued contacting the center")
			}
			if _, err := restarted.Join(context.Background(), "Keeper_01", 7); !errors.Is(err, ranking.ErrDeletedState) {
				t.Fatalf("Join after deletion error = %v", err)
			}
		})
	}
}

func TestServiceUsesReportBanFlagWithoutAdditionalRequests(t *testing.T) {
	for _, mode := range []string{"banned", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			store := ranking.NewStore(openRankingDatabase(t))
			seedActiveState(t, store, 0)
			now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
			center := &centerStub{}
			center.report = func(context.Context, ranking.ReportCommand) (ranking.ReportReceipt, error) {
				return ranking.ReportReceipt{}, &ranking.CenterError{StatusCode: http.StatusGone, Code: "participant_deleted", Banned: mode == "banned"}
			}
			service, err := ranking.NewService(store, &aggregatorStub{latestID: 1}, center, ranking.WithClock(func() time.Time { return now }))
			if err != nil {
				t.Fatal(err)
			}
			if err := service.SyncNow(context.Background()); !errors.Is(err, ranking.ErrParticipantDeleted) {
				t.Fatalf("SyncNow error = %v", err)
			}
			status, err := service.Status(context.Background())
			if err != nil || status.Status != ranking.StatusDeleted || status.Banned != (mode == "banned") {
				t.Fatalf("status = %s, banned = %t, error = %v", status.Status, status.Banned, err)
			}
			if center.selfCalls != 1 || center.reportCalls != 1 {
				t.Fatalf("unexpected calls: self=%d report=%d", center.selfCalls, center.reportCalls)
			}
		})
	}
}

func TestServicePersistsBanFromRegistrationAndExit(t *testing.T) {
	for _, action := range []string{"registration", "exit"} {
		t.Run(action, func(t *testing.T) {
			store := ranking.NewStore(openRankingDatabase(t))
			center := &centerStub{}
			center.register = func(context.Context, ranking.RegistrationCommand) (ranking.ParticipantInfo, error) {
				return ranking.ParticipantInfo{}, &ranking.CenterError{StatusCode: http.StatusGone, Code: "participant_deleted", Banned: true}
			}
			center.delete = func(_ context.Context, command ranking.DeleteCommand) (ranking.DeleteReceipt, error) {
				return ranking.DeleteReceipt{ParticipantID: command.ParticipantID, Sequence: command.Sequence, DeletedAt: command.RequestedAt, Banned: true}, nil
			}
			service, err := ranking.NewService(store, &aggregatorStub{}, center)
			if err != nil {
				t.Fatal(err)
			}
			if action == "registration" {
				if _, err := service.Join(context.Background(), "Keeper_01", 7); !errors.Is(err, ranking.ErrParticipantDeleted) {
					t.Fatalf("Join error = %v", err)
				}
			} else {
				seedActiveState(t, store, 0)
				if _, err := service.Exit(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			status, err := service.Status(context.Background())
			if err != nil || status.Status != ranking.StatusDeleted || !status.Banned {
				t.Fatalf("status = %s, banned = %t, error = %v", status.Status, status.Banned, err)
			}
		})
	}
}
