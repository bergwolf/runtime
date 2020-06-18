// Copyright (c) 2018 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package virtcontainers

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	govmmQemu "github.com/intel/govmm/qemu"
	"github.com/kata-containers/runtime/virtcontainers/types"
	"github.com/stretchr/testify/assert"
)

func qemuConfig(machineType string) HypervisorConfig {
	return HypervisorConfig{
		HypervisorMachineType: machineType,
	}
}

func newTestQemu(machineType string) qemuArch {
	config := qemuConfig(machineType)
	return newQemuArch(config)
}

func TestQemuAmd64Capabilities(t *testing.T) {
	assert := assert.New(t)

	amd64 := newTestQemu(QemuPC)
	caps := amd64.capabilities()
	assert.True(caps.IsBlockDeviceHotplugSupported())

	amd64 = newTestQemu(QemuQ35)
	caps = amd64.capabilities()
	assert.True(caps.IsBlockDeviceHotplugSupported())

	amd64 = newTestQemu(QemuMicrovm)
	caps = amd64.capabilities()
	assert.False(caps.IsBlockDeviceHotplugSupported())
}

func TestQemuAmd64Bridges(t *testing.T) {
	assert := assert.New(t)
	amd64 := newTestQemu(QemuPC)
	len := 5

	amd64.bridges(uint32(len))
	bridges := amd64.getBridges()
	assert.Len(bridges, len)

	for i, b := range bridges {
		id := fmt.Sprintf("%s-bridge-%d", types.PCI, i)
		assert.Equal(types.PCI, b.Type)
		assert.Equal(id, b.ID)
		assert.NotNil(b.Devices)
	}

	amd64 = newTestQemu(QemuQ35)
	amd64.bridges(uint32(len))
	bridges = amd64.getBridges()
	assert.Len(bridges, len)

	for i, b := range bridges {
		id := fmt.Sprintf("%s-bridge-%d", types.PCI, i)
		assert.Equal(types.PCI, b.Type)
		assert.Equal(id, b.ID)
		assert.NotNil(b.Devices)
	}

	amd64 = newTestQemu(QemuMicrovm)
	amd64.bridges(uint32(len))
	bridges = amd64.getBridges()
	assert.Nil(bridges)

	amd64 = newTestQemu(QemuQ35 + QemuPC)
	amd64.bridges(uint32(len))
	bridges = amd64.getBridges()
	assert.Nil(bridges)
}

func TestQemuAmd64CPUModel(t *testing.T) {
	assert := assert.New(t)
	amd64 := newTestQemu(QemuPC)

	expectedOut := defaultCPUModel
	model := amd64.cpuModel()
	assert.Equal(expectedOut, model)

	amd64.disableNestingChecks()
	base, ok := amd64.(*qemuAmd64)
	assert.True(ok)
	base.vmFactory = true
	expectedOut = defaultCPUModel + ",vmx=off"
	model = amd64.cpuModel()
	assert.Equal(expectedOut, model)
}

func TestQemuAmd64MemoryTopology(t *testing.T) {
	assert := assert.New(t)
	amd64 := newTestQemu(QemuPC)
	memoryOffset := 1024

	hostMem := uint64(100)
	mem := uint64(120)
	slots := uint8(10)
	expectedMemory := govmmQemu.Memory{
		Size:   fmt.Sprintf("%dM", mem),
		Slots:  slots,
		MaxMem: fmt.Sprintf("%dM", hostMem+uint64(memoryOffset)),
	}

	m := amd64.memoryTopology(mem, hostMem, slots)
	assert.Equal(expectedMemory, m)
}

func TestQemuAmd64AppendImage(t *testing.T) {
	assert := assert.New(t)

	f, err := ioutil.TempFile("", "img")
	assert.NoError(err)
	defer func() { _ = f.Close() }()
	defer func() { _ = os.Remove(f.Name()) }()

	imageStat, err := f.Stat()
	assert.NoError(err)

	// save default supportedQemuMachines options
	machinesCopy := make([]govmmQemu.Machine, len(supportedQemuMachines))
	assert.Equal(len(supportedQemuMachines), copy(machinesCopy, supportedQemuMachines))

	cfg := qemuConfig(QemuPC)
	cfg.ImagePath = f.Name()
	cfg.DisableImageNvdimm = false
	amd64 := newQemuArch(cfg)
	for _, m := range amd64.(*qemuAmd64).supportedQemuMachines {
		assert.Contains(m.Options, qemuNvdimmOption)
	}

	expectedOut := []govmmQemu.Device{
		govmmQemu.Object{
			Driver:   govmmQemu.NVDIMM,
			Type:     govmmQemu.MemoryBackendFile,
			DeviceID: "nv0",
			ID:       "mem0",
			MemPath:  f.Name(),
			Size:     (uint64)(imageStat.Size()),
		},
	}

	devices, err := amd64.appendImage(nil, f.Name())
	assert.NoError(err)
	assert.Equal(expectedOut, devices)

	// restore default supportedQemuMachines options
	assert.Equal(len(supportedQemuMachines), copy(supportedQemuMachines, machinesCopy))

	cfg.DisableImageNvdimm = true
	amd64 = newQemuArch(cfg)
	for _, m := range amd64.(*qemuAmd64).supportedQemuMachines {
		assert.NotContains(m.Options, qemuNvdimmOption)
	}

	found := false
	devices, err = amd64.appendImage(nil, f.Name())
	assert.NoError(err)
	for _, d := range devices {
		if b, ok := d.(govmmQemu.BlockDevice); ok {
			assert.Equal(b.Driver, govmmQemu.VirtioBlock)
			assert.True(b.ShareRW)
			found = true
		}
	}
	assert.True(found)

	// restore default supportedQemuMachines options
	assert.Equal(len(supportedQemuMachines), copy(supportedQemuMachines, machinesCopy))
}

func TestQemuAmd64AppendBridges(t *testing.T) {
	var devices []govmmQemu.Device
	assert := assert.New(t)

	// check PC
	amd64 := newTestQemu(QemuPC)

	amd64.bridges(1)
	bridges := amd64.getBridges()
	assert.Len(bridges, 1)

	devices = amd64.appendBridges(devices)
	assert.Len(devices, 1)

	expectedOut := []govmmQemu.Device{
		govmmQemu.BridgeDevice{
			Type:    govmmQemu.PCIBridge,
			Bus:     defaultPCBridgeBus,
			ID:      bridges[0].ID,
			Chassis: 1,
			SHPC:    true,
			Addr:    "2",
		},
	}

	assert.Equal(expectedOut, devices)

	// Check Q35
	amd64 = newTestQemu(QemuQ35)

	amd64.bridges(1)
	bridges = amd64.getBridges()
	assert.Len(bridges, 1)

	devices = []govmmQemu.Device{}
	devices = amd64.appendBridges(devices)
	assert.Len(devices, 1)

	expectedOut = []govmmQemu.Device{
		govmmQemu.BridgeDevice{
			Type:    govmmQemu.PCIBridge,
			Bus:     defaultBridgeBus,
			ID:      bridges[0].ID,
			Chassis: 1,
			SHPC:    true,
			Addr:    "2",
		},
	}

	assert.Equal(expectedOut, devices)
}

func TestQemuAmd64WithInitrd(t *testing.T) {
	assert := assert.New(t)

	cfg := qemuConfig(QemuPC)
	cfg.InitrdPath = "dummy-initrd"
	amd64 := newQemuArch(cfg)

	for _, m := range amd64.(*qemuAmd64).supportedQemuMachines {
		assert.NotContains(m.Options, qemuNvdimmOption)
	}
}

func TestQemuAmd64Microvm(t *testing.T) {
	assert := assert.New(t)

	cfg := qemuConfig(QemuMicrovm)
	amd64 := newQemuArch(cfg)
	assert.False(cfg.DisableImageNvdimm)

	for _, m := range amd64.(*qemuAmd64).supportedQemuMachines {
		assert.NotContains(m.Options, qemuNvdimmOption)
	}

	assert.False(amd64.supportGuestMemoryHotplug())
}

func TestQemuAmd64Iommu(t *testing.T) {
	assert := assert.New(t)

	config := qemuConfig(QemuQ35)
	config.IOMMU = true
	qemu := newQemuArch(config)

	p := qemu.kernelParameters(false)
	assert.Contains(p, Param{"intel_iommu", "on"})

	m, err := qemu.machine()

	assert.NoError(err)
	assert.Contains(m.Options, "kernel_irqchip=split")
}
