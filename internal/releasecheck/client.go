package releasecheck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var stableV2 = regexp.MustCompile(`^v2\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// DownloadVersion 只读取当前正式 v2 版本的公开附件，不附带应用鉴权或身份信息。
func DownloadVersion(ctx context.Context, client *http.Client, version string) error {
	if !stableV2.MatchString(version) {
		return fmt.Errorf("not a stable v2 release")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://github.com/Willxup/cpa-usage-keeper/releases/download/"+version+"/version.txt", nil)
	if err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("version file HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1025))
	if err != nil {
		return err
	}
	if len(body) > 1024 || strings.TrimSpace(string(body)) != version {
		return fmt.Errorf("version file content mismatch")
	}
	return nil
}
