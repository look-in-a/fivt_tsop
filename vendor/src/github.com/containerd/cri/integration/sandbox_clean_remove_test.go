/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package integration

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/containerd/containerd"
	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
	"golang.org/x/sys/unix"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
)

func TestSandboxCleanRemove(t *testing.T) {
	ctx := context.Background()
	t.Logf("Create a sandbox")
	sbConfig := PodSandboxConfig("sandbox", "clean-remove")
	sb, err := runtimeService.RunPodSandbox(sbConfig, *runtimeHandler)
	require.NoError(t, err)
	defer func() {
		// Make sure the sandbox is cleaned up in any case.
		runtimeService.StopPodSandbox(sb)
		runtimeService.RemovePodSandbox(sb)
	}()

	t.Logf("Should not be able to remove the sandbox when it's still running")
	assert.Error(t, runtimeService.RemovePodSandbox(sb))

	t.Logf("Delete sandbox task from containerd")
	cntr, err := containerdClient.LoadContainer(ctx, sb)
	require.NoError(t, err)
	task, err := cntr.Task(ctx, nil)
	require.NoError(t, err)
	// Kill the task with signal SIGKILL, once the task exited,
	// the TaskExit event will trigger the task.Delete().
	err = task.Kill(ctx, syscall.SIGKILL, containerd.WithKillAll)
	require.NoError(t, err)

	t.Logf("Sandbox state should be NOTREADY")
	assert.NoError(t, Eventually(func() (bool, error) {
		status, err := runtimeService.PodSandboxStatus(sb)
		if err != nil {
			return false, err
		}
		return status.GetState() == runtime.PodSandboxState_SANDBOX_NOTREADY, nil
	}, time.Second, 30*time.Second), "sandbox state should become NOTREADY")

	t.Logf("Should not be able to remove the sandbox when netns is not closed")
	assert.Error(t, runtimeService.RemovePodSandbox(sb))

	t.Logf("Should be able to remove the sandbox after properly stopped")
	assert.NoError(t, runtimeService.StopPodSandbox(sb))
	assert.NoError(t, runtimeService.RemovePodSandbox(sb))
}

func TestSandboxRemoveWithoutIPLeakage(t *testing.T) {
	const hostLocalCheckpointDir = "/var/lib/cni"

	t.Logf("Make sure host-local ipam is in use")
	config, err := CRIConfig()
	require.NoError(t, err)
	fs, err := ioutil.ReadDir(config.NetworkPluginConfDir)
	require.NoError(t, err)
	require.NotEmpty(t, fs)
	f := filepath.Join(config.NetworkPluginConfDir, fs[0].Name())
	cniConfig, err := ioutil.ReadFile(f)
	require.NoError(t, err)
	if !strings.Contains(string(cniConfig), "host-local") {
		t.Skip("host-local ipam is not in use")
	}

	t.Logf("Create a sandbox")
	sbConfig := PodSandboxConfig("sandbox", "remove-without-ip-leakage")
	sb, err := runtimeService.RunPodSandbox(sbConfig, *runtimeHandler)
	require.NoError(t, err)
	defer func() {
		// Make sure the sandbox is cleaned up in any case.
		runtimeService.StopPodSandbox(sb)
		runtimeService.RemovePodSandbox(sb)
	}()

	t.Logf("Get pod information")
	status, info, err := SandboxInfo(sb)
	require.NoError(t, err)
	ip := status.GetNetwork().GetIp()
	require.NotEmpty(t, ip)
	require.NotNil(t, info.RuntimeSpec.Linux)
	var netNS string
	for _, n := range info.RuntimeSpec.Linux.Namespaces {
		if n.Type == runtimespec.NetworkNamespace {
			netNS = n.Path
		}
	}
	require.NotEmpty(t, netNS, "network namespace should be set")

	t.Logf("Should be able to find the pod ip in host-local checkpoint")
	checkIP := func(ip string) bool {
		found := false
		filepath.Walk(hostLocalCheckpointDir, func(_ string, info os.FileInfo, _ error) error {
			if info != nil && info.Name() == ip {
				found = true
			}
			return nil
		})
		return found
	}
	require.True(t, checkIP(ip))

	t.Logf("Kill sandbox container")
	require.NoError(t, KillPid(int(info.Pid)))

	t.Logf("Unmount network namespace")
	require.NoError(t, unix.Unmount(netNS, unix.MNT_DETACH))

	t.Logf("Network namespace should be closed")
	_, info, err = SandboxInfo(sb)
	require.NoError(t, err)
	assert.True(t, info.NetNSClosed)

	t.Logf("Remove network namespace")
	require.NoError(t, os.RemoveAll(netNS))

	t.Logf("Network namespace should still be closed")
	_, info, err = SandboxInfo(sb)
	require.NoError(t, err)
	assert.True(t, info.NetNSClosed)

	t.Logf("Sandbox state should be NOTREADY")
	assert.NoError(t, Eventually(func() (bool, error) {
		status, err := runtimeService.PodSandboxStatus(sb)
		if err != nil {
			return false, err
		}
		return status.GetState() == runtime.PodSandboxState_SANDBOX_NOTREADY, nil
	}, time.Second, 30*time.Second), "sandbox state should become NOTREADY")

	t.Logf("Should still be able to find the pod ip in host-local checkpoint")
	assert.True(t, checkIP(ip))

	t.Logf("Should be able to stop and remove the sandbox")
	assert.NoError(t, runtimeService.StopPodSandbox(sb))
	assert.NoError(t, runtimeService.RemovePodSandbox(sb))

	t.Logf("Should not be able to find the pod ip in host-local checkpoint")
	assert.False(t, checkIP(ip))
}
