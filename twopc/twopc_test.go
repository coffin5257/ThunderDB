/*
 * Copyright 2018 The ThunderDB Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package twopc

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"net"

	"errors"

	log "github.com/sirupsen/logrus"
	"github.com/thunderdb/ThunderDB/crypto/etls"
	"github.com/thunderdb/ThunderDB/rpc"
)

type RaftTxState int
type TestPolicy int

const (
	Initailized RaftTxState = iota
	Prepared
	Committed
	RolledBack
)

const (
	AllGood TestPolicy = iota
	FailOnPrepare
	FailOnCommit
)

var (
	nodes  []Worker
	policy TestPolicy
	pass   = "DU>p~[/dd2iImUs*"
)

type RaftTxID uint64

type RaftNodeRPCServer struct {
	server *rpc.Server
	addr   string

	mu    sync.Mutex // Protects following fields
	txid  RaftTxID
	state RaftTxState
}

type RaftNode struct {
	RaftNodeRPCServer

	conn   *etls.CryptoConn
	client *rpc.Client
}

type RaftWriteBatchReq struct {
	TxID RaftTxID
	Cmds []string
}

type RaftWriteBatchResp struct {
	ErrCode   int
	ErrString string
}

type RaftCommitReq struct {
	TxID RaftTxID
}

type RaftCommitResp struct {
	ErrCode   int
	ErrString string
}

type RaftRollbackReq struct {
	TxID RaftTxID
}

type RaftRollbackResp struct {
	ErrCode   int
	ErrString string
}

func NewRaftNode() (r *RaftNode, err error) {
	r = &RaftNode{
		RaftNodeRPCServer: RaftNodeRPCServer{
			txid:  0,
			state: Initailized,
		},
	}

	err = r.start()

	if err != nil {
		return nil, err
	}

	return r, err
}

var simpleCipherHandler etls.CipherHandler = func(conn net.Conn) (cryptoConn *etls.CryptoConn, err error) {
	cipher := etls.NewCipher([]byte(pass))
	cryptoConn = etls.NewConn(conn, cipher, nil)
	return
}

func (r *RaftNode) start() (err error) {
	// Start a local RPC server to simulate a Raft node
	addr := "127.0.0.1:0"

	l, err := etls.NewCryptoListener("tcp", addr, simpleCipherHandler)
	r.addr = l.Addr().String()

	if err != nil {
		return err
	}

	r.server, err = rpc.NewServerWithService(rpc.ServiceMap{"Raft": &r.RaftNodeRPCServer})

	if err != nil {
		return err
	}

	r.server.SetListener(l)
	go r.server.Serve()

	return nil
}

func (r *RaftNode) stop() {
	r.server.Stop()
}

func (r *RaftNodeRPCServer) RPCPrepare(req *RaftWriteBatchReq, resp *RaftWriteBatchResp) (
	err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state == Prepared && r.txid != req.TxID {
		resp.ErrCode = 1
		resp.ErrString = "raft node is in inconsistent state"
	}

	if policy == FailOnPrepare {
		resp.ErrCode = 2
		resp.ErrString = fmt.Sprintf("failed to prepare for txid %d", req.TxID)
		return nil
	}

	r.txid = req.TxID
	r.state = Prepared
	return nil
}

func (r *RaftNodeRPCServer) RPCCommit(req *RaftCommitReq, resp *RaftCommitResp) (err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state != Prepared || r.txid != req.TxID {
		resp.ErrCode = 1
		resp.ErrString = "raft node is in inconsistent state"
	}

	if policy == FailOnCommit {
		resp.ErrCode = 2
		resp.ErrString = fmt.Sprintf("failed to commit for txid %d", req.TxID)
		return nil
	}

	r.state = Committed
	return nil
}

func (r *RaftNodeRPCServer) RPCRollback(req *RaftRollbackReq, resp *RaftRollbackResp) (err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state != Prepared || r.txid != req.TxID {
		resp.ErrCode = 1
		resp.ErrString = "raft node is in inconsistent state"
	}

	r.state = RolledBack
	return nil
}

func (r *RaftNode) Prepare(ctx context.Context, wb WriteBatch) (err error) {
	log.Debugf("executing 2pc: addr = %s, phase = prepare", r.addr)
	defer log.Debugf("2pc result: addr = %s, phase = prepare, result = %v", r.addr, err)

	rwb, ok := wb.(*RaftWriteBatchReq)

	if !ok {
		err = fmt.Errorf("unexpected WriteBatch type")
		return err
	}

	cipher := etls.NewCipher([]byte(pass))
	conn, err := etls.Dial("tcp", r.addr, cipher)

	if err != nil {
		return err
	}

	client, err := rpc.InitClientConn(conn)

	if err != nil {
		return err
	}

	d, ok := ctx.Deadline()

	if ok {
		err = conn.SetDeadline(d)

		if err != nil {
			return err
		}
	}

	resp := new(RaftWriteBatchResp)
	err = client.Call("Raft.RPCPrepare", &rwb, resp)

	if err != nil {
		return err
	}

	if resp.ErrCode > 0 {
		err = fmt.Errorf(resp.ErrString)
	}

	return err
}

func (r *RaftNode) Commit(ctx context.Context, wb WriteBatch) (err error) {
	log.Debugf("executing 2pc: addr = %s, phase = commit", r.addr)
	defer log.Debugf("2pc result: addr = %s, phase = commit, result = %v", r.addr, err)

	rwb, ok := wb.(*RaftWriteBatchReq)

	if !ok {
		err = fmt.Errorf("unexpected WriteBatch type")
		return err
	}

	cipher := etls.NewCipher([]byte(pass))
	conn, err := etls.Dial("tcp", r.addr, cipher)

	if err != nil {
		return err
	}

	client, err := rpc.InitClientConn(conn)

	if err != nil {
		return err
	}

	d, ok := ctx.Deadline()

	if ok {
		err = conn.SetDeadline(d)

		if err != nil {
			return err
		}
	}

	resp := new(RaftCommitResp)
	err = client.Call("Raft.RPCCommit", &RaftCommitReq{rwb.TxID}, resp)

	if err != nil {
		return err
	}

	if resp.ErrCode > 0 {
		err = fmt.Errorf(resp.ErrString)
	}

	return err
}

func (r *RaftNode) Rollback(ctx context.Context, wb WriteBatch) (err error) {
	log.Debugf("executing 2pc: addr = %s, phase = rollback", r.addr)
	defer log.Debugf("2pc result: addr = %s, phase = rollback, result = %v", r.addr, err)

	rwb, ok := wb.(*RaftWriteBatchReq)

	if !ok {
		err = fmt.Errorf("unexpected WriteBatch type")
		return err
	}

	cipher := etls.NewCipher([]byte(pass))
	conn, err := etls.Dial("tcp", r.addr, cipher)

	if err != nil {
		return err
	}

	client, err := rpc.InitClientConn(conn)

	if err != nil {
		return err
	}

	d, ok := ctx.Deadline()

	if ok {
		err = conn.SetDeadline(d)

		if err != nil {
			return err
		}
	}

	resp := new(RaftRollbackResp)
	err = client.Call("Raft.RPCRollback", &RaftRollbackReq{rwb.TxID}, resp)

	if err != nil {
		return err
	}

	if resp.ErrCode > 0 {
		err = fmt.Errorf(resp.ErrString)
	}

	return err
}

func testSetup() (err error) {
	log.SetLevel(log.DebugLevel)
	nodes = make([]Worker, 10)

	for index := 0; index < 10; index++ {
		nodes[index], err = NewRaftNode()

		if err != nil {
			return nil
		}
	}

	return nil
}

func testNodeReset() {
	for _, node := range nodes {
		raft, ok := node.(*RaftNode)

		if ok {
			raft.state = Initailized
		}
	}
}

func testTearDown() {
	for _, node := range nodes {
		raft, ok := node.(*RaftNode)

		if ok {
			raft.stop()
		}
	}
}

func TestMain(m *testing.M) {
	err := testSetup()

	if err != nil {
		fmt.Println("Failed to setup test environment.")
		os.Exit(1)
	}

	defer testTearDown()
	os.Exit(m.Run())
}

func TestTwoPhaseCommit(t *testing.T) {
	c := NewCoordinator(&Options{timeout: 5 * time.Second})

	testNodeReset()

	policy = AllGood
	err := c.Put(nodes, &RaftWriteBatchReq{TxID: 0, Cmds: []string{"+1", "-3", "+10"}})

	if err != nil {
		t.Fatalf("Error occurred: %s", err.Error())
	}

	testNodeReset()

	policy = FailOnPrepare
	err = c.Put(nodes, &RaftWriteBatchReq{TxID: 1, Cmds: []string{"-3", "-4", "+1"}})

	if err == nil {
		t.Fatal("Unexpected result: returned nil while expecting an error")
	} else {
		t.Logf("Error occurred as expected: %s", err.Error())
	}

	testNodeReset()

	policy = FailOnCommit
	err = c.Put(nodes, &RaftWriteBatchReq{TxID: 2, Cmds: []string{"-5", "+9", "+1"}})

	if err == nil {
		t.Fatal("Unexpected result: returned nil while expecting an error")
	} else {
		t.Logf("Error occurred as expected: %s", err.Error())
	}
}

func TestTwoPhaseCommit_WithHooks(t *testing.T) {
	errorBeforePrepare := false
	beforePrepareError := errors.New("before prepare error")
	errorBeforeCommit := false
	beforeCommitError := errors.New("before commit error")
	errorBeforeRollback := false
	beforeRollbackError := errors.New("before rollback error")
	policy = AllGood

	c := NewCoordinator(&Options{
		timeout: 5 * time.Second,
		beforePrepare: func(cxt context.Context) error {
			if errorBeforePrepare {
				return beforePrepareError
			}

			return nil
		},
		beforeCommit: func(ctx context.Context) error {
			if errorBeforeCommit {
				return beforeCommitError
			}

			return nil
		},
		beforeRollback: func(ctx context.Context) error {
			if errorBeforeRollback {
				return beforeRollbackError
			}

			return nil
		}})

	testNodeReset()

	err := c.Put(nodes, &RaftWriteBatchReq{TxID: 0, Cmds: []string{"+1", "-3", "+10"}})
	if err != nil {
		t.Fatalf("Error occurred: %s", err.Error())
	}

	// error before prepare
	errorBeforePrepare = true
	errorBeforeCommit = false
	errorBeforeRollback = false

	testNodeReset()

	err = c.Put(nodes, &RaftWriteBatchReq{TxID: 1, Cmds: []string{"+1", "-3", "+10"}})
	if err == nil {
		t.Fatal("Unexpected result: returned nil while expecting an error")
	} else if err != beforePrepareError {
		t.Fatal("Unexpected result: beforePrepare error is expected")
	} else {
		t.Logf("Error occurred as expected: %s", err.Error())
	}

	// error before commit
	errorBeforePrepare = false
	errorBeforeCommit = true
	errorBeforeRollback = false

	testNodeReset()

	err = c.Put(nodes, &RaftWriteBatchReq{TxID: 2, Cmds: []string{"+1", "-3", "+10"}})
	if err == nil {
		t.Fatal("Unexpected result: returned nil while expecting an error")
	} else if err != beforeCommitError {
		t.Fatal("Unexpected result: beforeCommit error is expected")
	} else {
		t.Logf("Error occurred as expected: %s", err.Error())
	}

	// error before rollback
	errorBeforePrepare = false
	errorBeforeCommit = true
	errorBeforeRollback = true

	testNodeReset()

	err = c.Put(nodes, &RaftWriteBatchReq{TxID: 3, Cmds: []string{"+1", "-3", "+10"}})
	if err == nil {
		t.Fatal("Unexpected result: returned nil while expecting an error")
	} else if err != beforeCommitError {
		t.Fatal("Unexpected result: beforeCommit error is expected")
	} else {
		t.Logf("Error occurred as expected: %s", err.Error())
	}
}
