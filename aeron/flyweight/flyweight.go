/*
Copyright 2016-2018 Stanislav Liberman

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

package flyweight

import "github.com/corymonroe-coinbase/aeron-go/aeron/atomic"

type Flyweight interface {
	Wrap(*atomic.Buffer, int) Flyweight
	Size() int
	SetSize(int)
}

type FWBase struct {
	size int
}

func (m *FWBase) Size() int {
	return m.size
}

func (m *FWBase) SetSize(size int) {
	m.size = size
}
