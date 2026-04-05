package ep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Logger writes to both stdout and a log file simultaneously
type Logger struct {
	mu      sync.Mutex
	file    *os.File
	prefix  string
	entries []LogEntry
}

// LogEntry stores a structured log line for later analysis
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Stage     string `json:"stage"`
	Symbol    string `json:"symbol,omitempty"`
	Message   string `json:"message"`
}

// Global logger instance - initialized per run
var globalLogger *Logger

// InitLogger creates a new logger writing to the given file path.
// Call this once at the start of each scan or backtest run.
func InitLogger(logDir, filePrefix string) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log dir: %v", err)
	}

	ts := time.Now().Format("20060102_150405")
	filename := filepath.Join(logDir, fmt.Sprintf("%s_%s.log", filePrefix, ts))

	f, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %v", err)
	}

	l := &Logger{
		file:    f,
		prefix:  filePrefix,
		entries: []LogEntry{},
	}

	globalLogger = l

	l.Section("LOG STARTED")
	l.Infof("Log file: %s", filename)
	l.Infof("Run started at: %s", time.Now().Format(time.RFC3339))

	return l, nil
}

// Close flushes and closes the log file
func (l *Logger) Close() {
	if l.file != nil {
		l.Section("LOG ENDED")
		l.file.Sync()
		l.file.Close()
	}
}

// write is the core write method — writes to both console and file
func (l *Logger) write(level, stage, symbol, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	ts := time.Now().Format("15:04:05.000")
	line := fmt.Sprintf("[%s][%-5s][%-8s]", ts, level, stage)
	if symbol != "" {
		line += fmt.Sprintf("[%-8s]", symbol)
	}
	line += " " + msg

	// Console
	fmt.Println(line)

	// File
	if l.file != nil {
		fmt.Fprintln(l.file, line)
	}

	// Structured store
	l.entries = append(l.entries, LogEntry{
		Timestamp: ts,
		Level:     level,
		Stage:     stage,
		Symbol:    symbol,
		Message:   msg,
	})
}

// Section prints a prominent section divider
func (l *Logger) Section(title string) {
	bar := strings.Repeat("=", 80)
	l.write("INFO", "------", "", bar)
	l.write("INFO", "------", "", fmt.Sprintf("  %s", title))
	l.write("INFO", "------", "", bar)
}

// SubSection prints a lighter divider
func (l *Logger) SubSection(title string) {
	bar := strings.Repeat("-", 60)
	l.write("INFO", "------", "", bar)
	l.write("INFO", "------", "", fmt.Sprintf("  %s", title))
	l.write("INFO", "------", "", bar)
}

// Infof logs an informational message
func (l *Logger) Infof(stage, format string, args ...interface{}) {
	l.write("INFO", stage, "", fmt.Sprintf(format, args...))
}

// Debugf logs a debug message
func (l *Logger) Debugf(stage, symbol, format string, args ...interface{}) {
	l.write("DEBUG", stage, symbol, fmt.Sprintf(format, args...))
}

// Warnf logs a warning
func (l *Logger) Warnf(stage, symbol, format string, args ...interface{}) {
	l.write("WARN", stage, symbol, fmt.Sprintf(format, args...))
}

// Errorf logs an error
func (l *Logger) Errorf(stage, symbol, format string, args ...interface{}) {
	l.write("ERROR", stage, symbol, fmt.Sprintf(format, args...))
}

// Qualify logs a stock passing a stage
func (l *Logger) Qualify(stage, symbol, reason string) {
	l.write("PASS", stage, symbol, fmt.Sprintf("✅ QUALIFIED — %s", reason))
}

// Reject logs a stock failing a stage
func (l *Logger) Reject(stage, symbol, reason string) {
	l.write("FAIL", stage, symbol, fmt.Sprintf("❌ REJECTED — %s", reason))
}

// StockMetrics logs a key=value block for a single stock
func (l *Logger) StockMetrics(stage, symbol string, metrics map[string]interface{}) {
	l.write("DATA", stage, symbol, "--- Metrics ---")
	// Sort keys for deterministic output
	keys := make([]string, 0, len(metrics))
	for k := range metrics {
		keys = append(keys, k)
	}
	// Simple insertion sort to avoid importing "sort" only for this
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	for _, k := range keys {
		l.write("DATA", stage, symbol, fmt.Sprintf("  %-30s = %v", k, metrics[k]))
	}
}

// Progress logs a progress update
func (l *Logger) Progress(stage string, current, total int, extra string) {
	pct := 0.0
	if total > 0 {
		pct = float64(current) / float64(total) * 100
	}
	msg := fmt.Sprintf("Progress: %d / %d  (%.1f%%)", current, total, pct)
	if extra != "" {
		msg += "  " + extra
	}
	l.write("INFO", stage, "", msg)
}

// StageSummary logs a stage completion summary
func (l *Logger) StageSummary(stage string, passed, total int, elapsed time.Duration) {
	l.write("INFO", stage, "", fmt.Sprintf(
		"Stage complete: %d / %d passed  (%.1f%%)  elapsed=%v",
		passed, total, float64(passed)/float64(max(total, 1))*100, elapsed.Round(time.Millisecond),
	))
}

// Convenience: module-level wrappers so callers don't need to pass the logger around

func LogSection(title string) {
	if globalLogger != nil {
		globalLogger.Section(title)
	}
}

func LogSubSection(title string) {
	if globalLogger != nil {
		globalLogger.SubSection(title)
	}
}

func LogInfo(stage, format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.Infof(stage, format, args...)
	} else {
		fmt.Printf("[INFO][%s] %s\n", stage, fmt.Sprintf(format, args...))
	}
}

func LogDebug(stage, symbol, format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.Debugf(stage, symbol, format, args...)
	}
}

func LogWarn(stage, symbol, format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.Warnf(stage, symbol, format, args...)
	} else {
		fmt.Printf("[WARN][%s][%s] %s\n", stage, symbol, fmt.Sprintf(format, args...))
	}
}

func LogError(stage, symbol, format string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.Errorf(stage, symbol, format, args...)
	} else {
		fmt.Printf("[ERROR][%s][%s] %s\n", stage, symbol, fmt.Sprintf(format, args...))
	}
}

func LogQualify(stage, symbol, reason string) {
	if globalLogger != nil {
		globalLogger.Qualify(stage, symbol, reason)
	}
}

func LogReject(stage, symbol, reason string) {
	if globalLogger != nil {
		globalLogger.Reject(stage, symbol, reason)
	}
}

func LogMetrics(stage, symbol string, metrics map[string]interface{}) {
	if globalLogger != nil {
		globalLogger.StockMetrics(stage, symbol, metrics)
	}
}

func LogProgress(stage string, current, total int, extra string) {
	if globalLogger != nil {
		globalLogger.Progress(stage, current, total, extra)
	}
}

func LogStageSummary(stage string, passed, total int, elapsed time.Duration) {
	if globalLogger != nil {
		globalLogger.StageSummary(stage, passed, total, elapsed)
	}
}