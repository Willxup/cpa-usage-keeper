package api

import (
	"encoding/json"
	"net/http"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type usageImportHandler struct {
	db *gorm.DB
}

type usageImportRequest struct {
	Version int                    `json:"version"`
	Usage   cpa.StatisticsSnapshot `json:"usage"`
}

type usageImportResponse struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
	Total   int `json:"total"`
	Failed  int `json:"failed"`
}

func registerUsageImportRoute(router gin.IRoutes, db *gorm.DB) {
	handler := &usageImportHandler{db: db}
	router.POST("/usage/import", handler.importUsage)
}

func (h *usageImportHandler) importUsage(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var req usageImportRequest
	if err := json.Unmarshal(data, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	if req.Version != 0 && req.Version != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported version, must be 0 or 1"})
		return
	}

	snapshotRun := &models.SnapshotRun{
		FetchedAt:  time.Now().UTC(),
		CPABaseURL: "import",
		Status:     "pending",
		RawPayload: data,
	}
	if err := h.db.Create(snapshotRun).Error; err != nil {
		writeInternalError(c, "create snapshot run for import", err)
		return
	}

	events := service.FlattenUsageExport(snapshotRun.ID, cpa.UsageExport{
		Version:    req.Version,
		ExportedAt: time.Now().UTC(),
		Usage:      req.Usage,
	})

	if len(events) == 0 {
		if err := h.db.Model(&models.SnapshotRun{}).Where("id = ?", snapshotRun.ID).Updates(map[string]any{
			"status":          "completed",
			"inserted_events": 0,
			"deduped_events":  0,
		}).Error; err != nil {
			writeInternalError(c, "finalize imported snapshot run", err)
			return
		}
		c.JSON(http.StatusOK, usageImportResponse{})
		return
	}

	inserted, deduped, err := repository.InsertUsageEvents(h.db, events)
	if err != nil {
		writeInternalError(c, "insert imported usage events", err)
		return
	}

	failed := len(events) - inserted - deduped
	if err := h.db.Model(&models.SnapshotRun{}).Where("id = ?", snapshotRun.ID).Updates(map[string]any{
		"status":          "completed",
		"inserted_events": inserted,
		"deduped_events":  deduped,
	}).Error; err != nil {
		writeInternalError(c, "finalize imported snapshot run", err)
		return
	}

	c.JSON(http.StatusOK, usageImportResponse{
		Added:   inserted,
		Skipped: deduped,
		Total:   inserted + deduped + failed,
		Failed:  failed,
	})
}