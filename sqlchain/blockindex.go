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

package sqlchain

import (
	"encoding/binary"
	"sync"

	"github.com/thunderdb/ThunderDB/crypto/hash"
)

type blockNode struct {
	parent *blockNode
	hash   hash.Hash
	height int32
}

func newBlockNode(header *SignedHeader, parent *blockNode) (node *blockNode) {
	node = &blockNode{
		hash:   header.BlockHash,
		parent: nil,
		height: 0,
	}

	if parent != nil {
		node.parent = parent
		node.height = parent.height + 1
	}

	return node
}

func (bn *blockNode) initBlockNode(head *SignedHeader, parent *blockNode) {
	bn.hash = head.BlockHash
	bn.parent = nil
	bn.height = 0

	if parent != nil {
		bn.parent = parent
		bn.height = parent.height + 1
	}
}

func (bn *blockNode) ancestor(height int32) (ancestor *blockNode) {
	if height < 0 || height > bn.height {
		return nil
	}

	ancestor = bn

	for ancestor != nil && ancestor.height != height {
		ancestor = ancestor.parent
	}

	return ancestor
}

func (bn *blockNode) indexKey() []byte {
	indexKey := make([]byte, hash.HashSize+4)
	binary.BigEndian.PutUint32(indexKey[0:4], uint32(bn.height))
	copy(indexKey[4:hash.HashSize], bn.hash[:])
	return indexKey
}

type blockIndex struct {
	cfg *Config

	mu    sync.RWMutex
	index map[hash.Hash]*blockNode
}

func newBlockIndex(cfg *Config) (index *blockIndex) {
	index = &blockIndex{
		cfg:   cfg,
		index: make(map[hash.Hash]*blockNode),
	}

	return index
}

func (bi *blockIndex) AddBlock(newBlock *blockNode) {
	bi.mu.Lock()
	defer bi.mu.Unlock()
	bi.index[newBlock.hash] = newBlock
}

func (bi *blockIndex) HasBlock(hash *hash.Hash) (hasBlock bool) {
	bi.mu.RLock()
	defer bi.mu.RUnlock()
	_, hasBlock = bi.index[*hash]
	return hasBlock
}

func (bi *blockIndex) LookupNode(hash *hash.Hash) (b *blockNode) {
	bi.mu.RLock()
	defer bi.mu.RUnlock()
	b = bi.index[*hash]
	return b
}
