// Copyright 2016 VMware, Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package simulator

import (
	"time"

	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	"github.com/vmware/vic/pkg/vsphere/simulator/esx"
)

type HostSystem struct {
	mo.HostSystem
}

func NewHostSystem(host mo.HostSystem) *HostSystem {
	now := time.Now()

	host.Name = host.Summary.Config.Name
	host.Summary.Runtime = &host.Runtime
	host.Summary.Runtime.BootTime = &now

	hs := &HostSystem{
		HostSystem: host,
	}

	config := []struct {
		ref **types.ManagedObjectReference
		obj mo.Reference
	}{
		{&hs.ConfigManager.DatastoreSystem, &HostDatastoreSystem{Host: &hs.HostSystem}},
		{&hs.ConfigManager.NetworkSystem, NewHostNetworkSystem(&hs.HostSystem)},
		{&hs.ConfigManager.PatchManager, NewHostPatchManager(mo.HostPatchManager{})},
	}

	for _, c := range config {
		ref := Map.Put(c.obj).Reference()

		*c.ref = &ref
	}

	return hs
}

func hostParent(host *mo.HostSystem) *mo.ComputeResource {
	switch parent := Map.Get(*host.Parent).(type) {
	case *mo.ComputeResource:
		return parent
	case *ClusterComputeResource:
		return &parent.ComputeResource
	default:
		return nil
	}
}

// CreateDefaultESX creates a standalone ESX
// Adds objects of type: Datacenter, Network, ComputeResource, ResourcePool and HostSystem
func CreateDefaultESX(f *Folder) {
	dc := &esx.Datacenter
	f.putChild(dc)
	createDatacenterFolders(dc, false)

	host := NewHostSystem(esx.HostSystem)

	cr := &mo.ComputeResource{}
	cr.Self = *host.Parent
	cr.Name = host.Name
	cr.Host = append(cr.Host, host.Reference())
	Map.PutEntity(cr, host)

	pool := NewResourcePool()
	cr.ResourcePool = &pool.Self
	Map.PutEntity(cr, pool)
	pool.Owner = cr.Self

	Map.Get(dc.HostFolder).(*Folder).putChild(cr)
}

// CreateStandaloneHost uses esx.HostSystem as a template, applying the given spec
// and creating the ComputeResource parent and ResourcePool sibling.
func CreateStandaloneHost(f *Folder, spec types.HostConnectSpec) (*HostSystem, types.BaseMethodFault) {
	if spec.HostName == "" {
		return nil, &types.NoHost{}
	}

	pool := NewResourcePool()
	host := NewHostSystem(esx.HostSystem)

	host.Summary.Config.Name = spec.HostName
	host.Name = host.Summary.Config.Name
	host.Runtime.ConnectionState = types.HostSystemConnectionStateDisconnected

	cr := &mo.ComputeResource{}

	Map.PutEntity(cr, Map.NewEntity(host))

	Map.PutEntity(cr, Map.NewEntity(pool))

	cr.Name = host.Name
	cr.Host = append(cr.Host, host.Reference())
	cr.ResourcePool = &pool.Self

	f.putChild(cr)
	pool.Owner = cr.Self

	return host, nil
}

type destroyHostSystemTask struct {
	*HostSystem
}

func (c *destroyHostSystemTask) Run(task *Task) (types.AnyType, types.BaseMethodFault) {
	// Delete all VMs/VApps/Folders associated with hosts
	for _, vmReference := range c.HostSystem.Vm {
		switch vm := Map.Get(vmReference.Reference()).(type) {
		case *VirtualMachine:
			vm.DestroyTask(&types.Destroy_Task{})
		case *VirtualApp:
			vm.DestroyTask(&types.Destroy_Task{})
		case *ResourcePool:
			vm.DestroyTask(&types.Destroy_Task{})
		}
	}

	// Delete Associated Vswitch & NetworkSystem
	hostNetworkSystem := Map.Get(c.HostSystem.ConfigManager.NetworkSystem.Reference()).(*HostNetworkSystem)

	for _, vswitch := range hostNetworkSystem.NetworkInfo.Vswitch {
		hostNetworkSystem.RemoveVirtualSwitch(&types.RemoveVirtualSwitch{VswitchName: vswitch.Name})
	}

	// Delete the Datastores of type VMFS

	hostDatastoreSystem := Map.Get(c.HostSystem.ConfigManager.DatastoreSystem.Reference()).(*HostDatastoreSystem)
	for _, dsReference := range c.HostSystem.Datastore {
		dataStore := Map.Get(dsReference.Reference()).(*Datastore)
		if len(dataStore.Host) == 1 {
			hostDatastoreSystem.DestroyHostDatastore(&types.DestroyDatastore{This: dsReference.Reference()})
		} else {
			hostDatastoreSystem.RemoveHostDatastore(&types.RemoveDatastore{This: dsReference.Reference()})
		}
	}

	for _, networkRef := range c.HostSystem.Network {
		switch network := Map.Get(networkRef.Reference()).(type) {
		case *mo.DistributedVirtualPortgroup:
			dvs := Map.Get(network.Config.DistributedVirtualSwitch.Reference()).(*VmwareDistributedVirtualSwitch)
			req := types.ReconfigureDvs_Task{
				Spec: &types.VMwareDVSConfigSpec{
					DVSConfigSpec: types.DVSConfigSpec{
						Host: []types.DistributedVirtualSwitchHostMemberConfigSpec{
							types.DistributedVirtualSwitchHostMemberConfigSpec{
								Operation: string(types.ConfigSpecOperationRemove),
								Host:      c.HostSystem.Reference(),
							},
						},
					},
				},
			}
			dvs.ReconfigureDvsTask(&req)
		}
	}

	return nil, nil
}

func (h *HostSystem) DestroyTask(c *types.Destroy_Task) soap.HasFault {
	r := &methods.Destroy_TaskBody{}

	task := NewTask(&destroyHostSystemTask{h})

	r.Res = &types.Destroy_TaskResponse{
		Returnval: task.Self,
	}

	task.Run()

	return r
}

// Enter Maintenance Mode
type enterMaintenanceModeTask struct {
	*HostSystem
}

func (c *enterMaintenanceModeTask) Run(task *Task) (types.AnyType, types.BaseMethodFault) {
	if !c.HostSystem.Runtime.InMaintenanceMode {
		c.HostSystem.Runtime.InMaintenanceMode = true
		Map.Put(c.HostSystem)
		return nil, nil
	}
	return nil, &types.InvalidState{}
}

func (h *HostSystem) EnterMaintenanceModeTask(c *types.EnterMaintenanceMode_Task) soap.HasFault {
	r := &methods.EnterMaintenanceMode_TaskBody{}

	task := NewTask(&enterMaintenanceModeTask{h})

	r.Res = &types.EnterMaintenanceMode_TaskResponse{
		Returnval: task.Self,
	}

	task.Run()

	return r
}

// Exit Maintenance Mode
type exitMaintenanceModeTask struct {
	*HostSystem
}

func (c *exitMaintenanceModeTask) Run(task *Task) (types.AnyType, types.BaseMethodFault) {
	if !c.HostSystem.Runtime.InMaintenanceMode {
		c.HostSystem.Runtime.InMaintenanceMode = false
		Map.Put(c.HostSystem)
		return nil, nil
	}
	return nil, &types.InvalidState{}
}

func (h *HostSystem) ExitMaintenanceModeTask(c *types.ExitMaintenanceMode_Task) soap.HasFault {
	r := &methods.ExitMaintenanceMode_TaskBody{}

	task := NewTask(&exitMaintenanceModeTask{h})

	r.Res = &types.ExitMaintenanceMode_TaskResponse{
		Returnval: task.Self,
	}

	task.Run()

	return r
}

// Poweroff
type shutdownHostTask struct {
	*HostSystem
}

func (c *shutdownHostTask) Run(task *Task) (types.AnyType, types.BaseMethodFault) {
	if c.HostSystem.HostSystem.Runtime.PowerState != types.HostSystemPowerStatePoweredOff {
		c.HostSystem.HostSystem.Runtime.PowerState = types.HostSystemPowerStatePoweredOff
		return nil, nil
	}
	return nil, &types.InvalidState{}
}

func (h *HostSystem) ShutdownHostTask(c *types.ShutdownHost_Task) soap.HasFault {
	r := &methods.ShutdownHost_TaskBody{}

	task := NewTask(&shutdownHostTask{h})

	r.Res = &types.ShutdownHost_TaskResponse{
		Returnval: task.Self,
	}

	task.Run()

	return r
}
