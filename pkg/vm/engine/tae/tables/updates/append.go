// Copyright 2021 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package updates

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/matrixorigin/matrixone/pkg/container/types"

	"github.com/matrixorigin/matrixone/pkg/logutil"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/common"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/txnif"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/txn/txnbase"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/wal"
)

type AppendNode struct {
	*txnbase.TxnMVCCNode
	sync.RWMutex
	startRow uint32
	maxRow   uint32
	mvcc     *MVCCHandle
	id       *common.ID
}

func CompareAppendNode(e, o txnbase.MVCCNode) int {
	return e.(*AppendNode).Compare(o.(*AppendNode).TxnMVCCNode)
}

func MockAppendNode(ts types.TS, startRow, maxRow uint32, mvcc *MVCCHandle) *AppendNode {
	return &AppendNode{
		TxnMVCCNode: &txnbase.TxnMVCCNode{
			Start: ts,
			End:   ts,
		},
		maxRow: maxRow,
		mvcc:   mvcc,
	}
}

func NewCommittedAppendNode(
	ts types.TS,
	startRow, maxRow uint32,
	mvcc *MVCCHandle) *AppendNode {
	return &AppendNode{
		TxnMVCCNode: &txnbase.TxnMVCCNode{
			Start: ts,
			End:   ts,
		},
		startRow: startRow,
		maxRow:   maxRow,
		mvcc:     mvcc,
	}
}

func NewAppendNode(
	txn txnif.AsyncTxn,
	startRow, maxRow uint32,
	mvcc *MVCCHandle) *AppendNode {
	var ts types.TS
	if txn != nil {
		ts = txn.GetPrepareTS()
	}
	n := &AppendNode{
		TxnMVCCNode: &txnbase.TxnMVCCNode{
			Start:   ts,
			Prepare: ts,
			End:     txnif.UncommitTS,
			Txn:     txn,
		},
		startRow: startRow,
		maxRow:   maxRow,
		mvcc:     mvcc,
	}
	return n
}

func NewEmptyAppendNode() txnbase.MVCCNode {
	return &AppendNode{
		TxnMVCCNode: &txnbase.TxnMVCCNode{},
	}
}
func (node *AppendNode) CloneAll() txnbase.MVCCNode {
	panic("todo")
}
func (node *AppendNode) CloneData() txnbase.MVCCNode {
	panic("todo")
}
func (node *AppendNode) Update(txnbase.MVCCNode) {
	panic("todo")
}
func (node *AppendNode) GeneralDesc() string {
	return fmt.Sprintf("%s;StartRow=%d MaxRow=%d", node.TxnMVCCNode.String(), node.startRow, node.maxRow)
}
func (node *AppendNode) GeneralString() string {
	return node.GeneralDesc()
}
func (node *AppendNode) GeneralVerboseString() string {
	return node.GeneralDesc()
}

func (node *AppendNode) SetLogIndex(idx *wal.Index) {
	node.TxnMVCCNode.SetLogIndex(idx)
}
func (node *AppendNode) GetID() *common.ID {
	return node.id
}
func (node *AppendNode) OnReplayCommit(ts types.TS) {
	node.End = ts
}
func (node *AppendNode) GetCommitTS() types.TS {
	return node.GetEnd()
}
func (node *AppendNode) IsCommitted() bool {
	node.RLock()
	defer node.RUnlock()
	return node.IsCommittedLocked()
}
func (node *AppendNode) IsCommittedLocked() bool {
	return node.TxnMVCCNode.IsCommitted()
}
func (node *AppendNode) GetStartRow() uint32 { return node.startRow }
func (node *AppendNode) GetMaxRow() uint32 {
	return node.maxRow
}
func (node *AppendNode) SetMaxRow(row uint32) {
	node.maxRow = row
}

func (node *AppendNode) PrepareCommit() error {
	node.Lock()
	defer node.Unlock()
	_, err := node.TxnMVCCNode.PrepareCommit()
	return err
}

func (node *AppendNode) ApplyCommit(index *wal.Index) error {
	node.Lock()
	defer node.Unlock()
	if node.IsCommittedLocked() {
		panic("AppendNode | ApplyCommit | LogicErr")
	}
	node.TxnMVCCNode.ApplyCommit(index)
	if node.mvcc != nil {
		logutil.Debugf("Set MaxCommitTS=%v, MaxVisibleRow=%d", node.GetEndLocked(), node.GetMaxRow())
		node.mvcc.SetMaxVisible(node.GetEndLocked())
	}
	// logutil.Infof("Apply1Index %s TS=%d", index.String(), n.commitTs)
	listener := node.mvcc.GetAppendListener()
	if listener == nil {
		return nil
	}
	return listener(node)
}

func (node *AppendNode) ApplyRollback(index *wal.Index) (err error) {
	node.Lock()
	defer node.Unlock()
	node.TxnMVCCNode.ApplyRollback(index)
	return
}

func (node *AppendNode) WriteTo(w io.Writer) (n int64, err error) {
	cn, err := w.Write(txnbase.MarshalID(node.mvcc.GetID()))
	if err != nil {
		return
	}
	n += int64(cn)
	if err = binary.Write(w, binary.BigEndian, node.startRow); err != nil {
		return
	}
	n += 4
	if err = binary.Write(w, binary.BigEndian, node.maxRow); err != nil {
		return
	}
	n += 4
	var sn int64
	sn, err = node.TxnMVCCNode.WriteTo(w)
	if err != nil {
		return
	}
	n += sn
	return
}

func (node *AppendNode) ReadFrom(r io.Reader) (n int64, err error) {
	var sn int
	buf := make([]byte, txnbase.IDSize)
	if sn, err = r.Read(buf); err != nil {
		return
	}
	n = int64(sn)
	node.id = txnbase.UnmarshalID(buf)
	if err = binary.Read(r, binary.BigEndian, &node.startRow); err != nil {
		return
	}
	n += 4
	if err = binary.Read(r, binary.BigEndian, &node.maxRow); err != nil {
		return
	}
	n += 4
	var sn2 int64
	sn2, err = node.TxnMVCCNode.ReadFrom(r)
	if err != nil {
		return
	}
	n += sn2
	return
}

func (node *AppendNode) PrepareRollback() (err error) {
	node.mvcc.Lock()
	defer node.mvcc.Unlock()
	node.mvcc.DeleteAppendNodeLocked(node)
	return
}
func (node *AppendNode) MakeCommand(id uint32) (cmd txnif.TxnCmd, err error) {
	cmd = NewAppendCmd(id, node)
	return
}
func (node *AppendNode) GetEnd() types.TS {
	node.RLock()
	defer node.RUnlock()
	return node.GetEndLocked()
}
func (node *AppendNode) GetEndLocked() types.TS {
	return node.TxnMVCCNode.GetEnd()
}

func (node *AppendNode) NeedWaitCommitting(startTS types.TS) (bool, txnif.TxnReader) {
	node.RLock()
	defer node.RUnlock()
	return node.TxnMVCCNode.NeedWaitCommitting(startTS)
}
