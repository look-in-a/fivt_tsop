package mocker

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"github.com/bpowers/caps"
	"github.com/containerd/btrfs"
	"github.com/containerd/cgroups"
	"github.com/kata-containers/runtime/virtcontainers/pkg/nsenter"
	"github.com/matthewrsj/copy"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/vishvananda/netns"
)

func (m Mocker) init(dir string) {
	info, err := os.Stat(dir)
	wrapError(err)
	if !info.IsDir() {
		panic("source should be directory")
	}
	id, err := generateID(dir, imageMode)
	wrapError(err, "failed to generate image ID")
	fmt.Println(id)
	wrapError(createImagePath(dir, id))
	wrapError(copy.All(dir, btrfsPath+id))
}

func (m Mocker) pull(image string) {
	id, err := generateID(image, imageMode)
	wrapError(err, "Failed to generate image ID")
	fmt.Println(id)
	wrapError(createImagePath(image, id))
	err = downloadImage(image, id)
	if err != nil {
		m.rmi(id)
		wrapError(err)
	}
}

func (m Mocker) rmi(id string) {
	wrapError(checkID(id, imageMode))
	remove(id)
}

func (m Mocker) images() {
	fmt.Printf("%-15s %-15s %-15s %-15s\n", "IMAGE", "IMAGE ID", "SIZE", "CREATED")
	files, err := ioutil.ReadDir(btrfsPath)
	wrapError(err)
	for _, f := range files {
		if btrfs.IsSubvolume(btrfsPath + f.Name()); err == nil && isValidID(f.Name(), imageMode) {
			fmt.Printf("%-15s %-15s %-15s %-15s\n", f.Name(), getSource(f.Name()), dirSize(btrfsPath+f.Name()), f.ModTime().Format(time.Stamp))
		}
	}
}

func (m Mocker) ps() {
	fmt.Printf("%-20s %-20s %-20s %-20s %-20s\n", "CONTAINER ID", "IMAGE", "COMMAND", "CREATED", "STATUS")
	files, err := ioutil.ReadDir(btrfsPath)
	wrapError(err)
	for _, f := range files {
		if btrfs.IsSubvolume(btrfsPath + f.Name()); err == nil && isValidID(f.Name(), processMode) {
			fmt.Printf("%-20s %-20s %-20s %-20s %-20s\n", f.Name(), getSource(f.Name()), getCommand(f.Name()), f.ModTime().Format(time.Stamp), getStatus(f.Name()))
		}
	}
}

type worker struct {
	cmd     *exec.Cmd
	control cgroups.Cgroup
}

func (w *worker) Run() error {
	wrapError(syscall.Unmount("/proc", 0))
	cmd := exec.Command("mount", "-t", "proc", "proc", "/proc")
	cmd.Run()
	defer syscall.Unmount("/proc", 0)
	w.cmd.Run()
	return nil
}

func reexec(args []string) {
	psID := args[0]
	psPath := btrfsPath + psID
	command := args[1:]

	isAlive, control, processes := isAlive(psID)
	if !isAlive {
		panic("failed to recieve processes from cgroup")
	}
	pid := processes[0].Pid
	ns, err := netns.GetFromPid(pid)
	wrapError(err, "failed to get net namespace")
	wrapError(netns.Set(ns), "failed to set net namespace")

	wrapError(control.Add(cgroups.Process{
		Pid: os.Getpid(),
	}))

	caps.SetupChroot(psPath + "/")
	caps.EnterChroot(psPath + "/")

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = append(cmd.Env, "PATH=/bin:/usr/bin:/sbin:/usr/sbin")

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	w := worker{cmd, control}
	nsenter.NsEnter([]nsenter.Namespace{nsenter.Namespace{
		PID: pid, Type: nsenter.NSTypePID}, nsenter.Namespace{
		PID: pid, Type: nsenter.NSTypeUTS}}, w.Run)

	return
}

func rerun(args []string) {
	psID := args[0]
	psPath := btrfsPath + psID

	shares := uint64(1024)
	mem := int64(1024 * 1000000)
	control, err := cgroups.New(cgroups.V1, cgroups.StaticPath(psID), &specs.LinuxResources{
		CPU: &specs.LinuxCPU{
			Shares: &shares,
		},
		Memory: &specs.LinuxMemory{
			Limit: &mem,
		},
	})
	wrapError(control.Add(cgroups.Process{
		Pid: os.Getpid(),
	}))
	syscall.Sethostname([]byte("mocker"))
	caps.SetupChroot(psPath + "/")
	caps.EnterChroot(psPath + "/")

	f, err := os.OpenFile("/etc/resolv.conf", os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		fmt.Printf("failed open resolv.conf file: %v\n", err)
	}
	if _, err = f.WriteString("nameserver 8.8.8.8\n"); err != nil {
		fmt.Printf("failed to write to file: %v\n", err)
	}
	f.Close()

	syscall.Mount("proc", "proc", "proc", 0, "")
	defer syscall.Unmount("/proc", 0)

	f, err = os.Create(psID + ".log")
	defer f.Close()
	wrapError(err, "failed to create logfile")

	cmd := exec.Command(args[1], args[2:]...)
	cmd.Env = append(cmd.Env, "PATH=/bin:/usr/bin:/sbin:/usr/sbin")

	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(os.Stdout, f)
	cmd.Stderr = io.MultiWriter(os.Stderr, f)

	wrapError(cmd.Run(), "cmd.Start() failed")
}

func (m Mocker) run(imgID string, command ...string) {
	wrapError(checkID(imgID, imageMode))

	psID, err := generateID(imgID, processMode)
	wrapError(err, "Failed to generate image ID")
	fmt.Println(psID)

	psPath := btrfsPath + psID
	imgPath := btrfsPath + imgID

	runtime.LockOSThread()

	network := InitNetwork()
	defer network.Close()

	wrapError(btrfs.SubvolSnapshot(psPath, imgPath, false), "failed to create btrfs snapshot")

	fcmd, err := os.Create(psPath + "/" + psID + ".cmd")
	defer fcmd.Close()
	wrapError(err, "failed to create .cmd file")

	for _, arg := range command {
		if _, err := fcmd.WriteString(arg + " "); err != nil {
			wrapError(err, "failed to write to .cmd file")
		}
	}

	execCommand(psID, "rerun", command...)

	control, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(psID))
	wrapError(err, "failed to load cgroup")
	control.Delete()
}

func (m Mocker) exec(id string, commands ...string) {
	wrapError(checkID(id, processMode))
	execCommand(id, "reexec", commands...)
}

func (m Mocker) logs(id string) {
	wrapError(checkID(id, processMode))
	f, err := os.Open(btrfsPath + id + "/" + id + ".log")
	wrapError(err, "failed to open log file")
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
}

func (m Mocker) rm(id string) {
	wrapError(checkID(id, processMode))
	remove(id)
}

func (m Mocker) commit(psID, imgID string) {
	wrapError(checkID(psID, processMode))
	m.rmi(imgID)
	wrapError(btrfs.SubvolSnapshot(btrfsPath+imgID, btrfsPath+psID, false))
}
