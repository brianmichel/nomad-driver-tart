package driver

import (
	"context"
	"os"
	"time"

	cstructs "github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/drivers"
)

type mockProber struct {
	version string
	err     error
}

func (m *mockProber) Available(context.Context) (string, error) { return m.version, m.err }

type mockLister struct {
	vms []VMInfo
	err error
}

func (m *mockLister) List(context.Context) ([]VMInfo, error)          { return m.vms, m.err }
func (m *mockLister) Status(context.Context, string) (VMState, error) { return VMStateStopped, m.err }

type mockLifecycle struct {
	setupCalled  bool
	setupName    string
	startCalled  bool
	startName    string
	stopCalled   bool
	stopName     string
	deleteCalled bool
	deleteName   string
	cloneCalled  bool
	setResCalled bool
	setupReturn  string
	setupErr     error
	startReturn  int
	startErr     error
	stopErr      error
	deleteErr    error
	cloneErr     error
	setResErr    error
}

func (m *mockLifecycle) Setup(context.Context, VMConfig) (string, error) {
	m.setupCalled = true
	return m.setupReturn, m.setupErr
}
func (m *mockLifecycle) Start(context.Context, string, bool) (int, error) {
	m.startCalled = true
	return m.startReturn, m.startErr
}
func (m *mockLifecycle) Stop(context.Context, string, time.Duration) error {
	m.stopCalled = true
	return m.stopErr
}
func (m *mockLifecycle) Delete(context.Context, string) error {
	m.deleteCalled = true
	return m.deleteErr
}
func (m *mockLifecycle) CloneVM(context.Context, string, string) error {
	m.cloneCalled = true
	return m.cloneErr
}
func (m *mockLifecycle) SetVMResources(context.Context, string, int, int, int) error {
	m.setResCalled = true
	return m.setResErr
}

type mockCommander struct {
	execReturn int
	execErr    error
}

func (m *mockCommander) Exec(context.Context, VMConfig, ExecOptions) (int, error) {
	return m.execReturn, m.execErr
}

type mockNetworker struct {
	ip      string
	ipErr   error
	ipCalls int
}

func (m *mockNetworker) IPAddress(context.Context, string, *NetworkConfig) (string, error) {
	m.ipCalls++
	return m.ip, m.ipErr
}

type mockBuilder struct {
	startArgs   []string
	startErr    error
	pullArgs    []string
	needsDL     bool
	needsDLErr  error
	registryEnv []string
	registryErr error
}

func (m *mockBuilder) BuildStartArgs(VMConfig) ([]string, error) { return m.startArgs, m.startErr }
func (m *mockBuilder) BuildPullArgs(VMConfig) []string           { return m.pullArgs }
func (m *mockBuilder) PrepareRegistryEnv(context.Context, VMConfig) ([]string, error) {
	return m.registryEnv, m.registryErr
}
func (m *mockBuilder) NeedsImageDownload(context.Context, VMConfig) (bool, error) {
	return m.needsDL, m.needsDLErr
}

type stubExecutor struct {
	shutdownCalled bool
	shutdownSignal string
	shutdownAfter  time.Duration
}

func (s *stubExecutor) Launch(*executor.ExecCommand) (*executor.ProcessState, error) { return nil, nil }
func (s *stubExecutor) Wait(context.Context) (*executor.ProcessState, error)         { return nil, nil }
func (s *stubExecutor) Shutdown(signal string, gracePeriod time.Duration) error {
	s.shutdownCalled = true
	s.shutdownSignal = signal
	s.shutdownAfter = gracePeriod
	return nil
}
func (s *stubExecutor) UpdateResources(*drivers.Resources) error { return nil }
func (s *stubExecutor) Version() (*executor.ExecutorVersion, error) {
	return &executor.ExecutorVersion{}, nil
}
func (s *stubExecutor) Stats(context.Context, time.Duration) (<-chan *cstructs.TaskResourceUsage, error) {
	return nil, nil
}
func (s *stubExecutor) Signal(os.Signal) error { return nil }
func (s *stubExecutor) Exec(time.Time, string, []string) ([]byte, int, error) {
	return nil, 0, nil
}
func (s *stubExecutor) ExecStreaming(context.Context, []string, bool, drivers.ExecTaskStream) error {
	return nil
}
