package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const authFilesStatusWorkerLimit = 10

var ErrAuthFilesManagementValidation = errors.New("auth files management request validation failed")

type AuthFilesManagementClient interface {
	UpdateAuthFileStatus(ctx context.Context, name string, disabled bool) error
	DeleteAuthFiles(ctx context.Context, names []string) error
}

type AuthFilesManagementProvider interface {
	SetAuthFilesDisabled(ctx context.Context, names []string, disabled bool) (AuthFilesManagementResponse, error)
	DeleteAuthFiles(ctx context.Context, names []string) (AuthFilesManagementResponse, error)
}

type AuthFilesManagementResponse struct {
	Names    []string `json:"names"`
	Affected int      `json:"affected"`
}

type authFilesManagementService struct {
	client AuthFilesManagementClient
}

func NewAuthFilesManagementService(client AuthFilesManagementClient) AuthFilesManagementProvider {
	return &authFilesManagementService{client: client}
}

func (s *authFilesManagementService) SetAuthFilesDisabled(ctx context.Context, names []string, disabled bool) (AuthFilesManagementResponse, error) {
	cleanNames, err := cleanAuthFileNames(names)
	if err != nil {
		return AuthFilesManagementResponse{}, err
	}
	if s.client == nil {
		return AuthFilesManagementResponse{}, fmt.Errorf("auth files management client is not configured")
	}

	sem := make(chan struct{}, authFilesStatusWorkerLimit)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var updateErr error
	// CPA 的 status 接口一次只接收一个账号；这里在服务端批量 fan-out，并限制并发避免压垮上游。
	for _, name := range cleanNames {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				updateErr = joinAuthFilesManagementError(updateErr, ctx.Err())
				mu.Unlock()
				return
			}
			if err := s.client.UpdateAuthFileStatus(ctx, name, disabled); err != nil {
				mu.Lock()
				updateErr = joinAuthFilesManagementError(updateErr, fmt.Errorf("%s: %w", name, err))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if updateErr != nil {
		return AuthFilesManagementResponse{}, updateErr
	}
	return AuthFilesManagementResponse{Names: cleanNames, Affected: len(cleanNames)}, nil
}

func (s *authFilesManagementService) DeleteAuthFiles(ctx context.Context, names []string) (AuthFilesManagementResponse, error) {
	cleanNames, err := cleanAuthFileNames(names)
	if err != nil {
		return AuthFilesManagementResponse{}, err
	}
	if s.client == nil {
		return AuthFilesManagementResponse{}, fmt.Errorf("auth files management client is not configured")
	}
	if err := s.client.DeleteAuthFiles(ctx, cleanNames); err != nil {
		return AuthFilesManagementResponse{}, err
	}
	return AuthFilesManagementResponse{Names: cleanNames, Affected: len(cleanNames)}, nil
}

func joinAuthFilesManagementError(joined error, err error) error {
	if err == nil {
		return joined
	}
	if joined == nil {
		return err
	}
	if errors.Is(joined, context.Canceled) && errors.Is(err, context.Canceled) {
		return joined
	}
	if errors.Is(joined, context.DeadlineExceeded) && errors.Is(err, context.DeadlineExceeded) {
		return joined
	}
	return errors.Join(joined, err)
}

func cleanAuthFileNames(names []string) ([]string, error) {
	seen := make(map[string]struct{}, len(names))
	cleanNames := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		cleanNames = append(cleanNames, trimmed)
	}
	if len(cleanNames) == 0 {
		return nil, fmt.Errorf("%w: names are required", ErrAuthFilesManagementValidation)
	}
	return cleanNames, nil
}
