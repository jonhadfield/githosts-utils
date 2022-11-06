package githosts

import (
	"fmt"
	"github.com/pkg/errors"
	"os"
	"os/exec"
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

func isEmpty(clonedRepoPath string) (bool, error) {
	remoteHeadsCmd := exec.Command("git", "count-objects", "-v")
	remoteHeadsCmd.Dir = clonedRepoPath
	out, err := remoteHeadsCmd.CombinedOutput()
	if err != nil {
		return true, errors.Wrapf(err, "failed to count objects in %s", clonedRepoPath)
	}

	cmdOutput := strings.Split(string(out), "\n")
	var looseObjects bool
	var inPackObjects bool
	var matchingLinesFound int
	for _, line := range cmdOutput {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			switch fields[0] {
			case "count:":
				matchingLinesFound++
				looseObjects = fields[1] != "0"
			case "in-pack:":
				matchingLinesFound++
				inPackObjects = fields[1] != "0"
			}
		}
	}

	if matchingLinesFound != 2 {
		return false, fmt.Errorf("failed to get object counts from %s", clonedRepoPath)
	}

	if !looseObjects && !inPackObjects {
		return true, nil
	}

	return false, nil
}
