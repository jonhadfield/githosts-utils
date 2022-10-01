package githosts

import (
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

const (
	pathSep       = string(os.PathSeparator)
	backupDirMode = 0o755
)

func createDirIfAbsent(path string) error {
	return os.MkdirAll(path, backupDirMode)
}

func getTimestamp() string {
	t := time.Now()

	return t.Format(timeStampFormat)
}

func timeStampToTime(s string) (t time.Time, err error) {
	if len(s) != bundleTimestampChars {
		return time.Time{}, errors.New("invalid timestamp")
	}

	return time.Parse(timeStampFormat, s)
}

func stripTrailing(input string, toStrip string) string {
	if strings.HasSuffix(input, toStrip) {
		return input[:len(input)-len(toStrip)]
	}

	return input
}

func isEmpty(name string) (bool, error) {
	f, err := os.Open(name)

	defer func() {
		if cErr := f.Close(); cErr != nil {
			logger.Printf("warn: failed to close: %s", name)
		}
	}()

	if err != nil {
		return false, err
	}

	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}

	return false, err
}
