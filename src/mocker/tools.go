package mocker

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/codeclysm/extract"
	"github.com/containerd/btrfs"
	"github.com/containerd/cgroups"
)

type token struct {
	Token string
}

type Manifest struct {
	FsLayers []map[string]string
}

func checkPath(path string) error {
	subvs, err := btrfs.SubvolList(btrfsPath)
	if err != nil {
		return err
	}
	dstPath := btrfsPath + path
	for _, subv := range subvs {
		if dstPath == subv.Path {
			return nil
		}
	}
	return fmt.Errorf("path %s doesn't exist", path)
}

func checkID(path, mode string) error {
	if !isValidID(path, mode) {
		return fmt.Errorf("wrong id")
	}
	return checkPath(path)
}

func getManifest(token, image string) (Manifest, error) {
	var manifest Manifest
	client := &http.Client{}

	req, err := http.NewRequest("GET", dockerLibrary+image+"/manifests/latest", nil)
	if err != nil {
		fmt.Println(err)
		return manifest, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return manifest, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return manifest, err
	}

	json.Unmarshal(body, &manifest)
	return manifest, nil
}

func processLayer(i, token, image, id, hash string) error {
	client := &http.Client{}
	req, err := http.NewRequest("GET", dockerLibrary+image+"/blobs/"+hash, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	extract.Tar(context.Background(), gzReader, btrfsPath+id, nil)
	return nil
}

func getToken(image string) (string, error) {
	resp, err := http.Get(tokenService + image + ":pull")
	if err != nil {
		return "", err
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	var token token
	json.Unmarshal(body, &token)
	return token.Token, nil
}

func downloadImage(image, id string) error {
	token, err := getToken(image)
	if err != nil {
		return err
	}
	manifest, err := getManifest(token, image)
	if err != nil {
		return err
	}
	for i, layer := range manifest.FsLayers {
		var hash string
		for _, hash = range layer {
			break
		}
		if err = processLayer(fmt.Sprint(i), token, image, id, hash); err != nil {
			return err
		}
	}
	return nil
}

func generateID(image, mode string) (string, error) {
	var str string
	for {
		str = mode + fmt.Sprint(rand.Intn(10000))
		if err := checkPath(str); err != nil {
			break
		}
	}
	return str, nil
}

func remove(id string) {
	btrfs.SubvolDelete(btrfsPath + id)
}

func isValidID(id, mode string) bool {
	l := len(mode)
	if len(id) > l && id[:l] == mode {
		return true
	}
	return false
}

func createImagePath(image, id string) error {
	subvolPath := btrfsPath + id
	err := btrfs.SubvolCreate(subvolPath)
	if err != nil {
		return err
	}
	f, err := os.Create(subvolPath + "/" + "image.source")
	if err != nil {
		return err
	}
	f.Write([]byte(image))
	f.Close()
	return nil
}

func getSource(id string) string {
	f, err := os.Open(btrfsPath + id + "/" + "image.source")
	if err != nil {
		return unknownString
	}
	bytes, err := ioutil.ReadAll(f)
	if err != nil {
		return unknownString
	}
	return string(bytes)
}

func dirSize(path string) string {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	if err != nil {
		return unknownString
	}
	return fmt.Sprintf("%.2f", float64(size)/1024.0/1024.0) + "MB"
}

//private func from netlink lib
func randMacAddr() string {
	hw := make(net.HardwareAddr, 6)
	for i := 0; i < 6; i++ {
		hw[i] = byte(rand.Intn(255))
	}
	hw[0] &^= 0x1 // clear multicast bit
	hw[0] |= 0x2  // set local assignment bit (IEEE802)
	return hw.String()
}

func cut(str string, n int) string {
	if len(str) < n {
		return str
	}
	if n == 1 {
		return "…"
	}
	return str[:n-1] + "…"
}

func getCommand(ps string) string {
	f, err := os.Open(btrfsPath + ps + "/" + ps + ".cmd")
	defer f.Close()
	if err != nil {
		return unknownString
	}
	bytes, err := ioutil.ReadAll(f)
	if err != nil {
		return unknownString
	}
	return cut(string(bytes), 20)
}

func getStatus(ps string) string {
	isAlive, _, _ := isAlive(ps)
	if isAlive {
		return "RUN"
	}
	return "STOPPED"
}

func wrapError(err error, strs ...string) {
	panicMessage := strings.Join(strs, ":")
	if err != nil {
		panic(fmt.Errorf("Error %s: %v", panicMessage, err))
	}
}

func isAlive(psID string) (bool, cgroups.Cgroup, []cgroups.Process) {
	control, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(psID))
	if err != nil {
		return false, nil, nil
	}
	processes, err := control.Processes("pids", false)
	if err != nil || len(processes) == 0 {
		return false, nil, nil
	}
	return true, control, processes
}

func execCommand(psID, mode string, command ...string) {
	if len(command) == 0 {
		return
	}
	cmd := exec.Command("/proc/self/exe", append([]string{mode, psID},
		command...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	attrs := &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS, Unshareflags: syscall.CLONE_NEWNS}
	if mode == "rerun" {
		attrs.Cloneflags |= syscall.CLONE_NEWPID | syscall.CLONE_NEWUTS
	}
	cmd.SysProcAttr = attrs
	cmd.Run()
}
