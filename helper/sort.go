/*
Copyright (c) 2016 The Amdatu Foundation

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
package helper

import "bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"

// ByModificationDate implements sort.Interface for []Deployment
type DeploymentByModificationDate []*types.Deployment

func (a DeploymentByModificationDate) Len() int      { return len(a) }
func (a DeploymentByModificationDate) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a DeploymentByModificationDate) Less(i, j int) bool {
	return a[i].LastModified > a[j].LastModified
}

// ByModificationDate implements sort.Interface for []Descriptor
type DescriptorByModificationDate []*types.Descriptor

func (a DescriptorByModificationDate) Len() int      { return len(a) }
func (a DescriptorByModificationDate) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a DescriptorByModificationDate) Less(i, j int) bool {
	return a[i].LastModified > a[j].LastModified
}
