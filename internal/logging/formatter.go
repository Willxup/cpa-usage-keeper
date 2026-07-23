package logging

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/sirupsen/logrus"
)

const (
	ansiReset   = "\x1b[0m"
	ansiGray    = "\x1b[90m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiMagenta = "\x1b[35m"
)

var ansiSequencePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type keeperFormatter struct{}

func (keeperFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var line strings.Builder
	writeColored(&line, ansiGray, entry.Time.Format(time.RFC3339))
	writeColored(&line, ansiGray, " | ")
	writeColored(&line, levelColor(entry.Level), fmt.Sprintf("%-5s", levelLabel(entry.Level)))
	writeColored(&line, ansiGray, " | ")
	line.WriteString(escapeMessage(entry.Message))

	keys := make([]string, 0, len(entry.Data))
	for key := range entry.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		writeColored(&line, ansiGray, " | ")
		for index, key := range keys {
			if index > 0 {
				line.WriteByte(' ')
			}
			line.WriteString(formatFieldKey(key))
			line.WriteByte('=')
			line.WriteString(formatFieldValue(entry.Data[key]))
		}
	}
	line.WriteByte('\n')
	return []byte(line.String()), nil
}

func writeColored(target *strings.Builder, color, value string) {
	target.WriteString(color)
	target.WriteString(value)
	target.WriteString(ansiReset)
}

func levelColor(level logrus.Level) string {
	switch level {
	case logrus.PanicLevel, logrus.FatalLevel, logrus.ErrorLevel:
		return ansiRed
	case logrus.WarnLevel:
		return ansiYellow
	case logrus.InfoLevel:
		return ansiGreen
	case logrus.DebugLevel, logrus.TraceLevel:
		return ansiMagenta
	default:
		return ansiGray
	}
}

func levelLabel(level logrus.Level) string {
	if level == logrus.WarnLevel {
		return "warn"
	}
	return level.String()
}

func escapeMessage(message string) string {
	var escaped strings.Builder
	for _, character := range message {
		if !unicode.IsControl(character) && character != '\u2028' && character != '\u2029' {
			escaped.WriteRune(character)
			continue
		}
		quoted := strconv.QuoteRune(character)
		escaped.WriteString(quoted[1 : len(quoted)-1])
	}
	return escaped.String()
}

func formatFieldKey(key string) string {
	if !fieldValueNeedsQuoting(key) {
		return key
	}
	return strconv.Quote(key)
}

func formatFieldValue(value any) string {
	text := fmt.Sprint(value)
	if !fieldValueNeedsQuoting(text) {
		return text
	}
	return strconv.Quote(text)
}

func fieldValueNeedsQuoting(value string) bool {
	if value == "" {
		return true
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("-._/@^+", character) {
			continue
		}
		return true
	}
	return false
}

type ansiStrippingWriter struct {
	writer io.Writer
}

// NewPlainWriter 为文件等持久化目标移除 Keeper 控制台颜色。
func NewPlainWriter(writer io.Writer) io.Writer {
	return ansiStrippingWriter{writer: writer}
}

func (w ansiStrippingWriter) Write(content []byte) (int, error) {
	plain := ansiSequencePattern.ReplaceAll(content, nil)
	written, err := w.writer.Write(plain)
	consumed := coloredPrefixLength(content, written, len(plain))
	if err != nil {
		return consumed, err
	}
	if written != len(plain) {
		return consumed, io.ErrShortWrite
	}
	return len(content), nil
}

func coloredPrefixLength(content []byte, writtenPlain, totalPlain int) int {
	if writtenPlain <= 0 {
		return 0
	}
	if writtenPlain >= totalPlain {
		return len(content)
	}

	ansiRanges := ansiSequencePattern.FindAllIndex(content, -1)
	rangeIndex := 0
	plainBytes := 0
	for offset := 0; offset < len(content); {
		if rangeIndex < len(ansiRanges) && ansiRanges[rangeIndex][0] == offset {
			offset = ansiRanges[rangeIndex][1]
			rangeIndex++
			continue
		}
		offset++
		plainBytes++
		if plainBytes == writtenPlain {
			return offset
		}
	}
	return len(content)
}
