// pricing-restore 在停止 Keeper 写入后，从 M1 备份与故障库生成新的旧版数据库。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"cpa-usage-keeper/internal/backup"
)

func main() {
	backupPath := flag.String("backup", "", "verified M1 backup database path")
	faultPath := flag.String("fault", "", "stopped fault database path")
	outputPath := flag.String("out", "", "new restore database path (must not exist)")
	flag.Parse()
	if *backupPath == "" || *faultPath == "" || *outputPath == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: pricing-restore -backup M1.db -fault fault.db -out restored.db")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := backup.RestorePricingBackup(ctx, *backupPath, *faultPath, *outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("restore created: %s\nM1 inbox max ID: %d\nfault inbox max ID: %d\nreplayed inbox rows: %d\n", *outputPath, result.BackupInboxMaxID, result.FaultInboxMaxID, result.ReplayedRows)
}
