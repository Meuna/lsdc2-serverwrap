package internal

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
)

func zipToS3(bucket string, key string, root string, filenames []string) error {
	buf := bytes.Buffer{}

	w := zip.NewWriter(&buf)

	for _, fname := range filenames {
		err := zipFileRecursive(w, root, fname)
		if err != nil {
			fmt.Println(err)
		}
	}

	// For some reason, we can't defer closing the zip writer after uploading to S3
	if err := w.Close(); err != nil {
		fmt.Println(err)
	}

	return readToS3(bucket, key, bytes.NewReader(buf.Bytes()))
}

func unzipFromS3(bucket string, key string, root string, uid int, gid int) error {
	buf := manager.NewWriteAtBuffer([]byte{})
	err := writeFromS3(bucket, key, buf)
	if err != nil {
		return err
	}

	bufr := bytes.NewReader(buf.Bytes())

	r, err := zip.NewReader(bufr, bufr.Size())
	if err != nil {
		return err
	}

	for _, f := range r.File {
		err := unzipFile(f, root, uid, gid)
		if err != nil {
			fmt.Println(err)
		}
	}
	return nil
}

func zipFileRecursive(w *zip.Writer, root string, path string) error {
	fullPath := filepath.Join(root, path)
	zipName := path
	pathInfo, err := os.Stat(fullPath)

	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%v not a relative path to %v", path, root)
	}

	// Write zip header info
	header, _ := zip.FileInfoHeader(pathInfo)
	header.Method = zip.Deflate
	header.Name = zipName
	if pathInfo.IsDir() {
		header.Name += "/"
	}
	headerW, err := w.CreateHeader(header)
	if err != nil {
		return err
	}

	if !pathInfo.IsDir() {
		// Write file content
		f, err := os.Open(fullPath)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(headerW, f)
	} else {
		// Or recurse in the directory
		files, err := ioutil.ReadDir(fullPath)
		if err != nil {
			return err
		}
		for _, file := range files {
			err := zipFileRecursive(w, root, filepath.Join(zipName, file.Name()))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func unzipFile(f *zip.File, root string, uid int, gid int) error {
	dst := filepath.Join(root, f.Name)

	// Create directory tree
	if f.FileInfo().IsDir() {
		if err := os.MkdirAll(dst, os.ModePerm); err != nil {
			return err
		}
		return nil
	}

	if err := mkdirAllChown(filepath.Dir(dst), os.ModePerm, gid, uid); err != nil {
		return err
	}

	// Create a destination file for unzipped content
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer func() {
		dstFile.Close()
		os.Chown(dst, uid, gid)
	}()

	// Unzip the content of a file and copy it to the destination file
	zippedFile, err := f.Open()
	if err != nil {
		return err
	}
	defer zippedFile.Close()

	if _, err := io.Copy(dstFile, zippedFile); err != nil {
		return err
	}
	return nil
}

// Cheap vresion of os.MkdirAll, but with UID and GID arguments
func mkdirAllChown(path string, perm fs.FileMode, uid int, gid int) error {
	// Fast path: if we can tell whether path is a directory or file, stop with success or error.
	dir, err := os.Stat(path)
	if err == nil {
		if dir.IsDir() {
			return nil
		}
		return fmt.Errorf("mkdir: %v is a file", path)
	}

	// Slow path: make sure parent exists and then call Mkdir for path.
	parent := filepath.Dir(path)
	err = mkdirAllChown(parent, perm, uid, gid)
	if err != nil {
		return err
	}

	// Parent now exists; invoke Mkdir and use its result.
	err = os.Mkdir(path, perm)
	os.Chown(path, uid, gid)
	if err != nil {
		// Handle arguments like "foo/." by
		// double-checking that directory doesn't exist.
		dir, err1 := os.Lstat(path)
		if err1 == nil && dir.IsDir() {
			return nil
		}
		return err
	}
	return nil
}
