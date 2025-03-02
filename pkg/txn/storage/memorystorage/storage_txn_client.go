// Copyright 2022 Matrix Origin
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

package memorystorage

import (
	"context"

	"github.com/google/uuid"
	"github.com/matrixorigin/matrixone/pkg/pb/txn"
	"github.com/matrixorigin/matrixone/pkg/txn/client"
	"github.com/matrixorigin/matrixone/pkg/txn/clock"
	"github.com/matrixorigin/matrixone/pkg/txn/rpc"
	"github.com/matrixorigin/matrixone/pkg/txn/storage"
)

type StorageTxnClient struct {
	clock   clock.Clock
	storage storage.TxnStorage
}

func NewStorageTxnClient(
	clock clock.Clock,
	storage storage.TxnStorage,
) *StorageTxnClient {
	return &StorageTxnClient{
		clock:   clock,
		storage: storage,
	}
}

var _ client.TxnClient = new(StorageTxnClient)

func (s *StorageTxnClient) New(options ...client.TxnOption) (client.TxnOperator, error) {
	now, _ := s.clock.Now()
	meta := txn.TxnMeta{
		ID:         []byte(uuid.NewString()),
		SnapshotTS: now,
	}
	return &StorageTxnOperator{
		storage: s.storage,
		meta:    meta,
	}, nil
}

func (*StorageTxnClient) NewWithSnapshot(snapshot []byte) (client.TxnOperator, error) {
	panic("unimplemented")
}

type StorageTxnOperator struct {
	storage storage.TxnStorage
	meta    txn.TxnMeta
}

var _ client.TxnOperator = new(StorageTxnOperator)

func (s *StorageTxnOperator) ApplySnapshot(data []byte) error {
	panic("unimplemented")
}

func (s *StorageTxnOperator) Commit(ctx context.Context) error {
	return s.storage.Commit(ctx, s.meta)
}

func (s *StorageTxnOperator) Read(ctx context.Context, ops []txn.TxnRequest) (*rpc.SendResult, error) {

	// set op txn meta
	for i := range ops {
		ops[i].Txn = s.meta
	}

	result := &rpc.SendResult{}
	for _, op := range ops {
		txnResponse := txn.TxnResponse{
			RequestID:    op.RequestID,
			Txn:          &op.Txn,
			Method:       op.Method,
			Flag:         op.Flag,
			CNOpResponse: new(txn.CNOpResponse),
		}
		res, err := s.storage.Read(
			ctx,
			op.Txn,
			op.CNRequest.OpCode,
			op.CNRequest.Payload,
		)
		if err != nil {
			txnResponse.TxnError = &txn.TxnError{
				Message: err.Error(),
			}
		} else {
			payload, err := res.Read()
			if err != nil {
				panic(err)
			}
			txnResponse.CNOpResponse.Payload = payload
			res.Release()
		}
		result.Responses = append(result.Responses, txnResponse)
	}
	return result, nil
}

func (s *StorageTxnOperator) Rollback(ctx context.Context) error {
	return s.storage.Rollback(ctx, s.meta)
}

func (*StorageTxnOperator) Snapshot() ([]byte, error) {
	panic("unimplemented")
}

func (s *StorageTxnOperator) Txn() txn.TxnMeta {
	return s.meta
}

func (s *StorageTxnOperator) Write(ctx context.Context, ops []txn.TxnRequest) (*rpc.SendResult, error) {

	// set op txn meta
	for i := range ops {
		ops[i].Txn = s.meta
	}

	result := &rpc.SendResult{}
	for _, op := range ops {
		txnResponse := txn.TxnResponse{
			RequestID:    op.RequestID,
			Txn:          &op.Txn,
			Method:       op.Method,
			Flag:         op.Flag,
			CNOpResponse: new(txn.CNOpResponse),
		}
		payload, err := s.storage.Write(
			ctx,
			op.Txn,
			op.CNRequest.OpCode,
			op.CNRequest.Payload,
		)
		if err != nil {
			txnResponse.TxnError = &txn.TxnError{
				Message: err.Error(),
			}
		} else {
			txnResponse.CNOpResponse.Payload = payload
		}
		result.Responses = append(result.Responses, txnResponse)
	}
	return result, nil
}

func (s *StorageTxnOperator) WriteAndCommit(ctx context.Context, ops []txn.TxnRequest) (*rpc.SendResult, error) {

	// set op txn meta
	for i := range ops {
		ops[i].Txn = s.meta
	}

	result, err := s.Write(ctx, ops)
	if err != nil {
		return nil, err
	}
	if err := s.storage.Commit(ctx, s.meta); err != nil {
		return nil, err
	}
	return result, nil
}
