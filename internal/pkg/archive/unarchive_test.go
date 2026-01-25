package archive

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openshift/oc-mirror/v2/internal/pkg/common"

	"github.com/stretchr/testify/assert"
)

func TestUnArchiver_UnArchive(t *testing.T) {
	t.Run("unarchive with 2 archive: should pass", func(t *testing.T) {
		testFolder := t.TempDir()
		defer os.RemoveAll(testFolder)

		// Create a new tar archive file : for working-dir
		archive1FileName := fmt.Sprintf(archiveFileNameFormat, archiveFilePrefix, 1)
		archive1Path := filepath.Join(testFolder, archive1FileName)
		// to be closed by BuildArchive
		archive1File, err := os.Create(archive1Path)
		assert.NoError(t, err, "should not fail")
		err = prepareFakeTarWorkingDir(archive1File)
		assert.NoError(t, err, "should not fail")

		// Create a new tar archive file : for cache-dir
		archive2FileName := fmt.Sprintf(archiveFileNameFormat, archiveFilePrefix, 2)
		archive2Path := filepath.Join(testFolder, archive2FileName)
		// to be closed by BuildArchive
		archive2File, err := os.Create(archive2Path)
		assert.NoError(t, err, "should not fail")

		err = prepareFakeTarCacheDir(archive2File)
		assert.NoError(t, err, "should not fail")

		o, err := NewArchiveExtractor(testFolder, filepath.Join(testFolder, "dst", "working-dir"), filepath.Join(testFolder, "dst", "cache-dir"))
		if err != nil {
			t.Fatal(err)
		}
		err = o.Unarchive()
		assert.NoError(t, err)

		assert.DirExists(t, filepath.Join(testFolder, "dst", "working-dir"))
		assert.DirExists(t, filepath.Join(testFolder, "dst", "cache-dir"))
	})

	t.Run("unarchive with 1 archive: should pass", func(t *testing.T) {
		testFolder := t.TempDir()
		defer os.RemoveAll(testFolder)

		// Create a new tar archive file
		archiveFileName := fmt.Sprintf(archiveFileNameFormat, archiveFilePrefix, 1)
		archivePath := filepath.Join(testFolder, archiveFileName)
		// to be closed by BuildArchive
		archiveFile, err := os.Create(archivePath)
		assert.NoError(t, err, "should not fail")
		err = prepareFakeTar(archiveFile)
		assert.NoError(t, err, "should not fail")

		o, err := NewArchiveExtractor(testFolder, filepath.Join(testFolder, "dst", "working-dir"), filepath.Join(testFolder, "dst", "cache-dir"))
		assert.NoError(t, err)
		err = o.Unarchive()
		assert.NoError(t, err)

		assert.DirExists(t, filepath.Join(testFolder, "dst", "working-dir"))
		assert.DirExists(t, filepath.Join(testFolder, "dst", "cache-dir"))
	})
}

func TestUnArchiver_NoArchive(t *testing.T) {
	testFolder := t.TempDir()
	defer os.RemoveAll(testFolder)
	workingDir := t.TempDir()
	cacheDir := t.TempDir()
	_, err := NewArchiveExtractor(testFolder, workingDir, cacheDir)
	assert.ErrorContains(t, err, "no tar archives matching")
}

func TestUnArchiver_WorkingDirError(t *testing.T) {
	testFolder := t.TempDir()
	defer os.RemoveAll(testFolder)

	// Create a new tar archive file
	archiveFileName := fmt.Sprintf(archiveFileNameFormat, archiveFilePrefix, 1)
	archivePath := filepath.Join(testFolder, archiveFileName)
	// to be closed by BuildArchive
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("should not fail")
	}

	err = prepareDummyTarWorkingDir(archiveFile)
	assert.NoError(t, err, "should not fail")

	o, err := NewArchiveExtractor(testFolder, filepath.Join("/", "dst"), filepath.Join(testFolder, "dst"))
	if err != nil {
		t.Fatal(err)
	}
	err = o.Unarchive()
	// Test expects an error when trying to create directories at root level
	// The working dir creation itself may succeed (mkdir /dst), but extracting files
	// will fail when trying to create parent directories for files like /working-dir/test-file.txt
	assert.Error(t, err, "expected an error but got nil")
	if err != nil {
		// The error can be either:
		// 1. "unable to create working dir" if /dst creation fails
		// 2. "unable to create parent directory" if file extraction fails
		// Both indicate permission issues at root level
		assert.True(t,
			strings.Contains(err.Error(), "unable to create working dir") ||
				strings.Contains(err.Error(), "unable to create parent directory"),
			"unexpected error: %v", err)
	}
}

func TestUnArchiver_CacheDirError(t *testing.T) {
	testFolder := t.TempDir()
	defer os.RemoveAll(testFolder)

	// Create a new tar archive file with cache content
	archiveFileName := fmt.Sprintf(archiveFileNameFormat, archiveFilePrefix, 1)
	archivePath := filepath.Join(testFolder, archiveFileName)
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("should not fail")
	}

	err = prepareFakeTar(archiveFile)
	assert.NoError(t, err, "should not fail")

	// Create a read-only directory to use as cache dir parent
	// This ensures the test will fail consistently across different systems
	readOnlyDir := filepath.Join(testFolder, "readonly")
	err = os.Mkdir(readOnlyDir, 0755)
	assert.NoError(t, err, "should create readonly dir")

	// Make it read-only (remove write permissions)
	err = os.Chmod(readOnlyDir, 0555)
	assert.NoError(t, err, "should make dir read-only")
	defer os.Chmod(readOnlyDir, 0755) // restore permissions for cleanup

	// Try to create cache dir inside read-only directory
	cacheDir := filepath.Join(readOnlyDir, "cache")

	o, err := NewArchiveExtractor(testFolder, filepath.Join(testFolder, "dst"), cacheDir)
	if err != nil {
		t.Fatal(err)
	}

	err = o.Unarchive()
	t.Logf("Unarchive error: %v", err)

	// Test expects an error when trying to create cache directory inside read-only parent
	// This should fail consistently across all systems
	assert.Error(t, err, "expected an error but got nil")
	if err != nil {
		assert.Contains(t, err.Error(), "unable to create cache dir")
	}
}

func prepareFakeTarWorkingDir(tarFile *os.File) error {
	tarWriter := tar.NewWriter(tarFile)
	workingDirFake := common.TestFolder + "working-dir-fake"

	err := filepath.Walk(workingDirFake, func(path string, info os.FileInfo, incomingError error) error {
		if incomingError != nil {
			return incomingError
		}
		if info.IsDir() { // skip directories
			return nil
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return fmt.Errorf("error getting tar.FileInfoHeader %w", err)
		}

		// Use full path as name (FileInfoHeader only takes the basename)
		// If we don't do this the directory strucuture would
		// not be preserved
		// https://golang.org/src/archive/tar/common.go?#L626
		relativePathToAdd, err := filepath.Rel(workingDirFake, path)
		if err != nil {
			return err
		}
		header.Name = filepath.Join("working-dir", relativePathToAdd)

		// Write the header to the tar archive
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("error writing tar header %w ", err)
		}

		// Open the file for reading
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// Copy the file contents to the tar archive
		if _, err := io.Copy(tarWriter, file); err != nil {
			return fmt.Errorf("error copying tar files %w", err)
		}

		return nil
	})
	tarWriter.Close()
	if err != nil {
		return fmt.Errorf("error preparing fake tar %w", err)
	}

	return nil
}

func prepareFakeTarCacheDir(tarFile *os.File) error {
	cacheDirFake := common.TestFolder + "cache-fake"
	tarWriter := tar.NewWriter(tarFile)
	err := filepath.Walk(cacheDirFake, func(path string, info os.FileInfo, incomingError error) error {
		if incomingError != nil {
			return incomingError
		}
		if info.IsDir() { // skip directories
			return nil
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return fmt.Errorf("error getting the tar file info header %w", err)
		}

		// Use full path as name (FileInfoHeader only takes the basename)
		// If we don't do this the directory strucuture would
		// not be preserved
		// https://golang.org/src/archive/tar/common.go?#L626
		header.Name, err = filepath.Rel(cacheDirFake, path)
		if err != nil {
			return err
		}

		// Write the header to the tar archive
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("error writing tar header %w", err)
		}

		// Open the file for reading
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// Copy the file contents to the tar archive
		if _, err := io.Copy(tarWriter, file); err != nil {
			return fmt.Errorf("error copying tar files %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("error preparing fake tar %w", err)
	}
	tarWriter.Close()
	return nil
}

func prepareFakeTar(tarFile *os.File) error {
	err := prepareFakeTarWorkingDir(tarFile)
	if err != nil {
		return err
	}
	err = prepareFakeTarCacheDir(tarFile)
	return err
}

// prepareDummyTar creates a minimal tar archive with dummy content.
// This is a simple helper that doesn't depend on external test fixtures.
func prepareDummyTar(tarFile *os.File) error {
	tarWriter := tar.NewWriter(tarFile)
	defer tarWriter.Close()

	// Create a simple dummy file entry
	content := []byte("dummy test content")
	header := &tar.Header{
		Name: "test.txt",
		Mode: 0644,
		Size: int64(len(content)),
	}

	// Write the header
	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("error writing tar header: %w", err)
	}

	// Write the content
	if _, err := tarWriter.Write(content); err != nil {
		return fmt.Errorf("error writing tar content: %w", err)
	}

	return nil
}

// prepareDummyTarWorkingDir creates a tar archive with working-dir content.
// This is similar to prepareFakeTar but creates dummy content inline without
// depending on external test fixtures.
func prepareDummyTarWorkingDir(tarFile *os.File) error {
	tarWriter := tar.NewWriter(tarFile)
	defer tarWriter.Close()

	// Create a dummy file in the working-dir structure
	content := []byte("dummy working-dir content")
	header := &tar.Header{
		Name: "working-dir/test-file.txt",
		Mode: 0644,
		Size: int64(len(content)),
	}

	// Write the header
	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("error writing tar header: %w", err)
	}

	// Write the content
	if _, err := tarWriter.Write(content); err != nil {
		return fmt.Errorf("error writing tar content: %w", err)
	}

	return nil
}

func TestSanitizeArchivePath(t *testing.T) {
	cases := []struct {
		name        string
		basedir     string
		filepath    string
		expected    string
		expectedErr bool
	}{
		{
			name:     "absolute path",
			basedir:  "/workdir",
			filepath: "path/to/file",
			expected: "/workdir/path/to/file",
		},
		{
			name:     "relative to current path",
			basedir:  "./workdir",
			filepath: "path/to/file",
			expected: "workdir/path/to/file",
		},
		{
			name:     "current dir as '.'",
			basedir:  ".",
			filepath: "path/to/file",
			expected: "path/to/file",
		},
		{
			name:     "filepath starts with '.'",
			basedir:  ".",
			filepath: "./path/to/file",
			expected: "path/to/file",
		},
		{
			name:     "hidden file in '.'",
			basedir:  ".",
			filepath: ".hidden",
			expected: ".hidden",
		},
		{
			name:     "non-tainted '..'",
			basedir:  "/workdir",
			filepath: "../workdir/path/to/file",
			expected: "/workdir/path/to/file",
		},
		{
			name:        "tainted '..'",
			basedir:     ".",
			filepath:    "../../../../../../../../../../../../etc/shadow",
			expectedErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := sanitizeArchivePath(tc.basedir, tc.filepath)
			if tc.expectedErr {
				assert.ErrorContains(t, err, "content filepath is tainted")
			} else if assert.NoError(t, err, res) {
				assert.Equal(t, tc.expected, res)
			}
		})
	}
}
