package entities

// PricingMigrationState 只服务首次升级的持久恢复；手动重算不写入此表。
// baseline_json 和 cursors_json 后续按 schema_version=1 写入，结构阶段不伪造基线或游标。
type PricingMigrationState struct {
	ID             int64   `gorm:"primaryKey;autoIncrement:false;check:chk_pricing_migration_state_singleton,id = 1"`
	InitKind       string  `gorm:"column:init_kind;type:text;not null"`
	Phase          string  `gorm:"column:phase;type:text;not null"`
	BackupPath     *string `gorm:"column:backup_path;type:text"`
	BaselineJSON   *string `gorm:"column:baseline_json;type:text"`
	CursorsJSON    *string `gorm:"column:cursors_json;type:text"`
	SchemaComplete bool    `gorm:"column:schema_complete;not null;default:false"`
	DataComplete   bool    `gorm:"column:data_complete;not null;default:false"`
}

func (PricingMigrationState) TableName() string { return "pricing_migration_state" }
