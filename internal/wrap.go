package internal

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type Wrapped struct {
	Dir     string
	SigWith os.Signal
	uid     int
	gid     int

	persist bool
	files   []string
	bucket  string
	key     string
	zipFrom string
	zip     bool

	cl  []string
	cmd *exec.Cmd
}

func NewWrapped(cl []string) *Wrapped {
	return &Wrapped{cl: cl, SigWith: syscall.SIGTERM}
}

func (w *Wrapped) RunAs(uid int, gid int) *Wrapped {
	w.uid = uid
	w.gid = gid
	return w
}

func (w *Wrapped) PersistData(bucket string, key string, files []string, zip bool, zipFrom string) *Wrapped {
	if len(files) > 1 {
		zip = true
	}
	w.persist = true
	w.bucket = bucket
	w.key = key
	w.files = files
	w.zip = zip
	w.zipFrom = zipFrom

	return w
}

func (w *Wrapped) StartProcess() {
	log.Println("S3 download")
	if w.persist {
		err := w.retrieveData()
		if err != nil {
			fmt.Println(err)
		}
	}

	w.cmd = exec.Command(w.cl[0], w.cl[1:]...)
	if w.Dir != "" {
		w.cmd.Dir = w.Dir
	}
	if (w.uid != 0) || (w.gid != 0) {
		w.cmd.SysProcAttr = &syscall.SysProcAttr{}
		w.cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(w.uid), Gid: uint32(w.gid)}
	}
	if err := w.cmd.Start(); err != nil {
		log.Panic(err)
	}
	log.Println("Process started")
}

func (w *Wrapped) StopProcess() {
	w.cmd.Process.Signal(w.SigWith)
	w.cmd.Wait()

	// Small wait to sync file system
	time.Sleep(1 * time.Second)

	log.Println("S3 upload")
	if w.persist {
		err := w.archiveData()
		if err != nil {
			fmt.Println(err)
		}
	}
}

func (w *Wrapped) retrieveData() error {
	if w.zip {
		return unzipFromS3(w.bucket, w.key, w.zipFrom, w.uid, w.gid)
	} else {
		return downloadFromS3(w.bucket, w.key, w.files[0], w.uid, w.gid)
	}
}

func (w *Wrapped) archiveData() error {
	if w.zip {
		return zipToS3(w.bucket, w.key, w.zipFrom, w.files)
	} else {
		return uploadToS3(w.bucket, w.key, w.files[0])
	}
}
