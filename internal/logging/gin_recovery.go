package logging

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// NewGinRecovery 把 panic 原因和栈作为 Keeper 结构化字段输出，并保持 Gin 的 500/断连语义。
func NewGinRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			brokenConnection, recoveredErr := ginBrokenConnection(recovered)
			fields := logrus.Fields{
				"panic": fmt.Sprint(recovered),
				"stack": string(debug.Stack()),
			}
			if c.Request != nil {
				fields["method"] = c.Request.Method
				if c.Request.URL != nil {
					fields["path"] = c.Request.URL.Path
				}
			}
			if brokenConnection {
				fields["broken_pipe"] = true
			}
			logrus.WithFields(fields).Error("gin panic recovered")

			if brokenConnection {
				_ = c.Error(recoveredErr)
				c.Abort()
				return
			}
			c.AbortWithStatus(http.StatusInternalServerError)
		}()
		c.Next()
	}
}

func ginBrokenConnection(recovered any) (bool, error) {
	recoveredErr, ok := recovered.(error)
	if !ok {
		return false, nil
	}
	var networkErr *net.OpError
	if !errors.As(recoveredErr, &networkErr) {
		return false, recoveredErr
	}
	var syscallErr *os.SyscallError
	if !errors.As(networkErr, &syscallErr) {
		return false, recoveredErr
	}
	message := strings.ToLower(syscallErr.Error())
	return strings.Contains(message, "broken pipe") || strings.Contains(message, "connection reset by peer"), recoveredErr
}
