package githosts

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

func deleteBackupsDir(path string) error {
	return os.RemoveAll(path)
}

func TestPruneBackups(t *testing.T) {
	backupDir := path.Join(os.TempDir() + pathSep + "tmp_githosts-utils")
	defer deleteBackupsDir(backupDir)

	dfDir := path.Join(backupDir, "github.com", "go-soba", "repo0")
	assert.NoError(t, os.MkdirAll(dfDir, 0755), fmt.Sprintf("failed to create dummy files dir: %s", dfDir))

	dummyFiles := []string{"repo0.20200401111111.bundle", "repo0.20200201010111.bundle", "repo0.20200501010111.bundle", "repo0.20200401011111.bundle", "repo0.20200601011111.bundle"}
	var err error
	for _, df := range dummyFiles {
		dfPath := path.Join(dfDir, df)
		_, err = os.OpenFile(dfPath, os.O_RDONLY|os.O_CREATE, 0666)
		assert.NoError(t, err, fmt.Sprintf("failed to open file: %s", dfPath))
	}
	assert.NoError(t, pruneBackups(dfDir, 2))
	files, err := ioutil.ReadDir(dfDir)
	assert.NoError(t, err)
	var found int
	notExpectedPostPrune := []string{"repo0.20200401111111.bundle", "repo0.20200201010111.bundle", "repo0.20200401011111.bundle"}
	expectedPostPrune := []string{"repo0.20200501010111.bundle", "repo0.20200601011111.bundle"}

	for _, f := range files {
		assert.NotContains(t, notExpectedPostPrune, f.Name())
		assert.Contains(t, expectedPostPrune, f.Name())
		found++
	}
	if found != 2 {
		t.Errorf("three backup files were expected")
	}

}
