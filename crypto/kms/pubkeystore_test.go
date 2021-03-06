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

package kms

import (
	"testing"

	"os"

	"sync"

	"encoding/hex"

	"bytes"

	"reflect"

	log "github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/thunderdb/ThunderDB/crypto/asymmetric"
	"github.com/thunderdb/ThunderDB/pow/cpuminer"
	"github.com/thunderdb/ThunderDB/proto"
	"github.com/ugorji/go/codec"
)

const dbFile = ".test.db"

func TestDB(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	privKey1, pubKey1, _ := asymmetric.GenSecp256k1KeyPair()
	privKey2, pubKey2, _ := asymmetric.GenSecp256k1KeyPair()
	node1 := &proto.Node{
		ID:        proto.NodeID("node1"),
		Addr:      "",
		PublicKey: pubKey1,
		Nonce:     cpuminer.Uint256{},
	}
	node2 := &proto.Node{
		ID:        proto.NodeID("node2"),
		Addr:      "",
		PublicKey: pubKey2,
		Nonce:     cpuminer.Uint256{},
	}
	publicKeyBytes, _ := hex.DecodeString(BPPublicKeyStr)
	BPPublicKey, _ = asymmetric.ParsePubKey(publicKeyBytes)
	BPNode := &proto.Node{
		ID:        proto.NodeID(BPNodeID),
		Addr:      "",
		PublicKey: BPPublicKey,
		Nonce:     BPNonce,
	}

	Convey("Init db", t, func() {
		pks = nil
		os.Remove(dbFile)
		defer os.Remove(dbFile)
		InitPublicKeyStore(dbFile, BPNode)
		So(pks.bucket, ShouldNotBeNil)

		nodeInfo, err := GetNodeInfo(proto.NodeID(BPNodeID))
		log.Debugf("nodeInfo %v", nodeInfo)
		pubk, err := GetPublicKey(proto.NodeID(BPNodeID))
		So(pubk, ShouldNotBeNil)
		So(err, ShouldBeNil)
		So(pubk.IsEqual(BPPublicKey), ShouldBeTrue)

		pubk, err = GetPublicKey(proto.NodeID("not exist"))
		So(pubk, ShouldBeNil)
		So(err, ShouldEqual, ErrKeyNotFound)

		err = SetNode(nil)
		So(err, ShouldEqual, ErrNilNode)

		err = setNode(node1)
		So(err, ShouldBeNil)

		err = setNode(node2)
		So(err, ShouldBeNil)

		err = SetPublicKey(proto.NodeID(BPNodeID), BPNonce, BPPublicKey)
		So(err, ShouldBeNil)

		err = SetPublicKey(proto.NodeID(BPNodeID), cpuminer.Uint256{}, BPPublicKey)
		So(err, ShouldEqual, ErrNodeIDKeyNonceNotMatch)

		err = SetPublicKey(proto.NodeID("0"+BPNodeID), BPNonce, BPPublicKey)
		So(err, ShouldEqual, ErrNotValidNodeID)

		pubk, err = GetPublicKey(proto.NodeID("node1"))
		So(pubk, ShouldNotBeNil)
		So(err, ShouldBeNil)
		So(privKey1.PubKey().IsEqual(pubKey1), ShouldBeTrue)

		pubk, err = GetPublicKey(proto.NodeID("node2"))
		So(pubk, ShouldNotBeNil)
		So(err, ShouldBeNil)
		So(privKey2.PubKey().IsEqual(pubKey2), ShouldBeTrue)

		IDs, err := GetAllNodeID()
		So(err, ShouldBeNil)
		So(IDs, ShouldHaveLength, 3)
		So(IDs, ShouldContain, proto.NodeID("node1"))
		So(IDs, ShouldContain, proto.NodeID("node2"))
		So(IDs, ShouldContain, proto.NodeID(BPNodeID))

		err = DelNode(proto.NodeID("node2"))
		So(err, ShouldBeNil)

		err = DelNode(proto.NodeID("node2"))
		So(err, ShouldBeNil)

		pubk, err = GetPublicKey(proto.NodeID("node2"))
		So(pubk, ShouldBeNil)
		So(err, ShouldEqual, ErrKeyNotFound)

		err = removeBucket()
		So(err, ShouldBeNil)

		pubk, err = GetPublicKey(proto.NodeID("not exist"))
		So(pubk, ShouldBeNil)
		So(err, ShouldEqual, ErrBucketNotInitialized)

		err = setNode(node1)
		So(err, ShouldEqual, ErrBucketNotInitialized)

		err = DelNode(proto.NodeID("node2"))
		So(err, ShouldEqual, ErrBucketNotInitialized)

		IDs, err = GetAllNodeID()
		So(IDs, ShouldBeNil)
		So(err, ShouldEqual, ErrBucketNotInitialized)

		err = ResetBucket()
		So(err, ShouldBeNil)

		pubk, err = GetPublicKey(proto.NodeID("node2"))
		So(pubk, ShouldBeNil)
		So(err, ShouldEqual, ErrKeyNotFound)

		IDs, err = GetAllNodeID()
		So(IDs, ShouldBeNil)
		So(err, ShouldBeNil)
	})
}

func TestErrorPath(t *testing.T) {
	Convey("can not init db", t, func() {
		pks = nil
		PksOnce = sync.Once{}
		err := InitPublicKeyStore("/path/not/exist", nil)
		So(pks, ShouldBeNil)
		So(err, ShouldNotBeNil)
	})
}

func TestMarshalNode(t *testing.T) {
	Convey("marshal unmarshal node", t, func() {
		nodeInfo := &proto.Node{
			ID:        "abc",
			Addr:      "addr",
			PublicKey: nil,
			Nonce: cpuminer.Uint256{
				A: 1,
				B: 2,
				C: 3,
				D: 4,
			},
		}
		nodeBuf := new(bytes.Buffer)
		mh := &codec.MsgpackHandle{}
		enc := codec.NewEncoder(nodeBuf, mh)
		err := enc.Encode(nodeInfo)
		if err != nil {
			log.Errorf("encode error: %s", err)
		}

		nodeDec := proto.NewNode()
		reader := bytes.NewReader(nodeBuf.Bytes())
		dec := codec.NewDecoder(reader, mh)
		err = dec.Decode(nodeDec)

		So(reflect.DeepEqual(nodeDec, nodeInfo), ShouldBeTrue)
	})
}
