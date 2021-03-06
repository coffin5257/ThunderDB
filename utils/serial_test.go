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

package utils

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/thunderdb/ThunderDB/crypto/asymmetric"
	"github.com/thunderdb/ThunderDB/crypto/hash"
	"github.com/thunderdb/ThunderDB/proto"
)

var (
	testGoRoutines = 100
	testRounds     = 100
)

func init() {
	for i := 0; i < maxPooledBufferNumber; i++ {
		serializer.returnBuffer(make([]byte, pooledBufferLength))
	}
}

type testStruct struct {
	BoolField      bool
	Int8Field      int8
	Uint8Field     uint8
	Int16Field     int16
	Uint16Field    uint16
	Int32Field     int32
	Uint32Field    uint32
	Int64Field     int64
	Uint64Field    uint64
	StringField    string
	BytesField     []byte
	TimeField      time.Time
	NodeIDField    proto.NodeID
	HashField      hash.Hash
	PublicKeyField *asymmetric.PublicKey
	SignatureField *asymmetric.Signature
}

func (s *testStruct) randomize() {
	s.BoolField = (rand.Int()%2 == 0)
	s.Int8Field = (int8)(rand.Int())
	s.Uint8Field = (uint8)(rand.Int())
	s.Int16Field = (int16)(rand.Int())
	s.Uint16Field = (uint16)(rand.Int())
	s.Int32Field = rand.Int31()
	s.Uint32Field = rand.Uint32()
	s.Int64Field = rand.Int63()
	s.Uint64Field = rand.Uint64()

	// Randomize StringField
	slen := rand.Intn(2 * pooledBufferLength)
	buff := make([]byte, slen)
	rand.Read(buff)
	s.StringField = string(buff)

	// Randomize BytesField
	slen = rand.Intn(2 * pooledBufferLength)

	if slen == 0 {
		s.BytesField = nil
	} else {
		s.BytesField = make([]byte, slen)
		rand.Read(s.BytesField)
	}

	s.TimeField = time.Unix(0, rand.Int63()).UTC()

	// Randomize NodeIDField
	slen = rand.Intn(2 * pooledBufferLength)
	buff = make([]byte, slen)
	rand.Read(buff)
	s.NodeIDField = proto.NodeID(buff)

	rand.Read(s.HashField[:])

	// Randomize PublicKeyField and SignatureField
	priv, pub, err := asymmetric.GenSecp256k1KeyPair()

	if err != nil {
		panic(err)
	}

	s.PublicKeyField = pub
	s.SignatureField, err = priv.Sign(s.HashField[:])

	if err != nil {
		panic(err)
	}
}

func (s *testStruct) MarshalBinary() ([]byte, error) {
	buffer := bytes.NewBuffer(nil)

	if err := WriteElements(buffer, binary.BigEndian,
		s.BoolField,
		s.Int8Field,
		s.Uint8Field,
		s.Int16Field,
		s.Uint16Field,
		s.Int32Field,
		s.Uint32Field,
		s.Int64Field,
		s.Uint64Field,
		s.StringField,
		s.BytesField,
		s.TimeField,
		s.NodeIDField,
		s.HashField,
		s.PublicKeyField,
		s.SignatureField,
	); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func (s *testStruct) MarshalBinary2() ([]byte, error) {
	buffer := bytes.NewBuffer(nil)

	if err := WriteElements(buffer, binary.BigEndian,
		&s.BoolField,
		&s.Int8Field,
		&s.Uint8Field,
		&s.Int16Field,
		&s.Uint16Field,
		&s.Int32Field,
		&s.Uint32Field,
		&s.Int64Field,
		&s.Uint64Field,
		&s.StringField,
		&s.BytesField,
		&s.TimeField,
		&s.NodeIDField,
		&s.HashField,
		&s.PublicKeyField,
		&s.SignatureField,
	); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func (s *testStruct) UnmarshalBinary(b []byte) error {
	reader := bytes.NewReader(b)
	return ReadElements(reader, binary.BigEndian,
		&s.BoolField,
		&s.Int8Field,
		&s.Uint8Field,
		&s.Int16Field,
		&s.Uint16Field,
		&s.Int32Field,
		&s.Uint32Field,
		&s.Int64Field,
		&s.Uint64Field,
		&s.StringField,
		&s.BytesField,
		&s.TimeField,
		&s.NodeIDField,
		&s.HashField,
		&s.PublicKeyField,
		&s.SignatureField,
	)
}

func TestNullValueSerialization(t *testing.T) {
	ots := &testStruct{}
	// XXX(leventeliu): beware of the zero value flaw -- time.Time zero value (January 1, year 1,
	// 00:00:00.000000000 UTC) is out of range of the int64 (or uint64) Unix time.
	ots.TimeField = time.Unix(0, 0).UTC()
	rts := &testStruct{}

	for i := 0; i < testRounds; i++ {
		oenc, err := ots.MarshalBinary()

		if err != nil {
			t.Fatalf("Error occurred: %v", err)
		}

		ohash := hash.HashH(oenc)

		if err = rts.UnmarshalBinary(oenc); err != nil {
			t.Fatalf("Error occurred: %v", err)
		}

		if !reflect.DeepEqual(ots, rts) {
			t.Fatalf("Result not match: \n\tt1=%+v\n\tt2=%+v", ots, rts)
		}

		renc, err := rts.MarshalBinary2()

		if err != nil {
			t.Fatalf("Error occurred: %v", err)
		}

		rhash := hash.HashH(renc)

		if rhash != ohash {
			t.Fatalf("Hash result not match: %s v.s. %s", ohash.String(), rhash.String())
		}
	}
}

func TestSerialization(t *testing.T) {
	wg := &sync.WaitGroup{}

	for i := 0; i < testGoRoutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ots := &testStruct{}
			rts := &testStruct{}

			for i := 0; i < testRounds; i++ {
				ots.randomize()
				oenc, err := ots.MarshalBinary()

				if err != nil {
					t.Errorf("Error occurred: %v", err)
				}

				ohash := hash.HashH(oenc)

				if err = rts.UnmarshalBinary(oenc); err != nil {
					t.Errorf("Error occurred: %v", err)
				}

				if !rts.SignatureField.Verify(rts.HashField[:], rts.PublicKeyField) {
					t.Errorf("Failed to verify signature: hash=%s, sign=%+v, pub=%+v",
						rts.HashField.String(),
						rts.SignatureField,
						rts.PublicKeyField,
					)
				}

				if !reflect.DeepEqual(ots, rts) {
					t.Errorf("Result not match: t1=%+v, t2=%+v", ots, rts)
				}

				renc, err := rts.MarshalBinary2()

				if err != nil {
					t.Errorf("Error occurred: %v", err)
				}

				rhash := hash.HashH(renc)

				if rhash != ohash {
					t.Errorf("Hash result not match: %s v.s. %s", ohash.String(), rhash.String())
				}
			}
		}()
	}

	wg.Wait()
}

func BenchmarkMarshalBinary(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st := &testStruct{}
		st.randomize()
		b.StartTimer()

		if _, err := st.MarshalBinary(); err != nil {
			b.Fatalf("Error occurred: %v", err)
		}

		b.StopTimer()
	}
}

func BenchmarkMarshalBinary2(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st := &testStruct{}
		st.randomize()
		b.StartTimer()

		if _, err := st.MarshalBinary2(); err != nil {
			b.Fatalf("Error occurred: %v", err)
		}

		b.StopTimer()
	}
}

func BenchmarkUnmarshalBinary(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st := &testStruct{}
		st.randomize()
		enc, err := st.MarshalBinary2()

		if err != nil {
			b.Fatalf("Error occurred: %v", err)
		}

		b.StartTimer()

		if err = st.UnmarshalBinary(enc); err != nil {
			b.Fatalf("Error occurred: %v", err)
		}

		b.StopTimer()
	}
}
