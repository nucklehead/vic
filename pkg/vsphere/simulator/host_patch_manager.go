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
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

type HostPatchManager struct {
	mo.HostPatchManager
	VibsList []string
}

func NewHostPatchManager(patch mo.HostPatchManager) *HostPatchManager {

	manager := &HostPatchManager{
		HostPatchManager: patch,
	}
	return manager
}

// installHostPatchV2_Task
type installHostPatchV2Task struct {
	*HostPatchManager
	newVibs []string
}

func (c *installHostPatchV2Task) Run(task *Task) (types.AnyType, types.BaseMethodFault) {
	c.HostPatchManager.VibsList = append(c.HostPatchManager.VibsList, c.newVibs...)
	return nil, nil
}

func (h *HostPatchManager) InstallHostPatchV2Task(c *types.InstallHostPatchV2_Task) soap.HasFault {
	r := &methods.InstallHostPatchV2_TaskBody{}
	c.VibUrls = append(c.VibUrls, c.BundleUrls...)
	c.VibUrls = append(c.VibUrls, c.MetaUrls...)
	task := NewTask(&installHostPatchV2Task{h, c.VibUrls})

	r.Res = &types.InstallHostPatchV2_TaskResponse{
		Returnval: task.Self,
	}

	task.Run()

	return r
}
