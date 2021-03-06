/*
 * Copyright 2018 The ThunderDB Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the “License”);
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an “AS IS” BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	// Register go-sqlite3 engine.
	_ "github.com/mattn/go-sqlite3"

	"github.com/thunderdb/ThunderDB/twopc"
)

var (
	index = struct {
		sync.Mutex
		db map[string]*sql.DB
	}{
		db: make(map[string]*sql.DB),
	}
)

// ExecLog represents the execution log of sqlite.
type ExecLog struct {
	ConnectionID uint64
	SeqNo        uint64
	Timestamp    uint64
	Queries      []string
}

func openDB(dsn string) (db *sql.DB, err error) {
	// Rebuild DSN.
	d, err := NewDSN(dsn)

	if err != nil {
		return nil, err
	}

	d.AddParam("_journal_mode", "WAL")
	d.AddParam("_synchronous", "FULL")
	fdsn := d.Format()

	fn := d.GetFileName()
	mode, _ := d.GetParam("mode")
	cache, _ := d.GetParam("cache")

	if (fn == ":memory:" || mode == "memory") && cache != "shared" {
		// Return a new DB instance if it's in memory and private.
		db, err = sql.Open("sqlite3", fdsn)
		return
	}

	index.Lock()
	db, ok := index.db[d.filename]
	index.Unlock()

	if !ok {
		db, err = sql.Open("sqlite3", fdsn)

		if err != nil {
			return nil, err
		}

		index.Lock()
		index.db[d.filename] = db
		index.Unlock()
	}

	return
}

// TxID represents a transaction ID.
type TxID struct {
	ConnectionID uint64
	SeqNo        uint64
	Timestamp    uint64
}

func equalTxID(x, y *TxID) bool {
	return x.ConnectionID == y.ConnectionID && x.SeqNo == y.SeqNo && x.Timestamp == y.Timestamp
}

// Storage represents a underlying storage implementation based on sqlite3.
type Storage struct {
	sync.Mutex
	dsn     string
	db      *sql.DB
	tx      *sql.Tx // Current tx
	id      TxID
	queries []string
}

// New returns a new storage connected by dsn.
func New(dsn string) (st *Storage, err error) {
	db, err := openDB(dsn)

	if err != nil {
		return
	}

	return &Storage{
		dsn: dsn,
		db:  db,
	}, nil
}

// Prepare implements prepare method of two-phase commit worker.
func (s *Storage) Prepare(ctx context.Context, wb twopc.WriteBatch) (err error) {
	el, ok := wb.(*ExecLog)

	if !ok {
		return errors.New("unexpected WriteBatch type")
	}

	s.Lock()
	defer s.Unlock()

	if s.tx != nil {
		if equalTxID(&s.id, &TxID{el.ConnectionID, el.SeqNo, el.Timestamp}) {
			s.queries = el.Queries
			return nil
		}

		return fmt.Errorf("twopc: inconsistent state, currently in tx: "+
			"conn = %d, seq = %d, time = %d", s.id.ConnectionID, s.id.SeqNo, s.id.Timestamp)
	}

	s.tx, err = s.db.BeginTx(ctx, nil)

	if err != nil {
		return
	}

	s.id = TxID{el.ConnectionID, el.SeqNo, el.Timestamp}
	s.queries = el.Queries

	return nil
}

// Commit implements commit method of two-phase commit worker.
func (s *Storage) Commit(ctx context.Context, wb twopc.WriteBatch) (err error) {
	el, ok := wb.(*ExecLog)

	if !ok {
		return errors.New("unexpected WriteBatch type")
	}

	s.Lock()
	defer s.Unlock()

	if s.tx != nil {
		if equalTxID(&s.id, &TxID{el.ConnectionID, el.SeqNo, el.Timestamp}) {
			for _, q := range s.queries {
				_, err = s.tx.ExecContext(ctx, q)

				if err != nil {
					s.tx.Rollback()
					s.tx = nil
					s.queries = nil
					return
				}
			}

			return nil
		}

		return fmt.Errorf("twopc: inconsistent state, currently in tx: "+
			"conn = %d, seq = %d, time = %d", s.id.ConnectionID, s.id.SeqNo, s.id.Timestamp)
	}

	return errors.New("twopc: tx not prepared")
}

// Rollback implements rollback method of two-phase commit worker.
func (s *Storage) Rollback(ctx context.Context, wb twopc.WriteBatch) (err error) {
	el, ok := wb.(*ExecLog)

	if !ok {
		return errors.New("unexpected WriteBatch type")
	}

	s.Lock()
	defer s.Unlock()

	if !equalTxID(&s.id, &TxID{el.ConnectionID, el.SeqNo, el.Timestamp}) {
		return fmt.Errorf("twopc: inconsistent state, currently in tx: "+
			"conn = %d, seq = %d, time = %d", s.id.ConnectionID, s.id.SeqNo, s.id.Timestamp)
	}

	if s.tx != nil {
		s.tx.Rollback()
		s.tx = nil
		s.queries = nil
	}

	return nil
}
