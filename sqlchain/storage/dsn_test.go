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
	"testing"
)

func TestDSN(t *testing.T) {
	testStrings := []string{
		"",
		"file:test.db",
		"file::memory:?cache=shared&mode=memory",
		"file:test.db?p1=v1&p2=v2&p1=v3",
	}

	for _, s := range testStrings {
		dsn, err := NewDSN(s)

		if err != nil {
			t.Errorf("Error occurred: %v", err)
			continue
		}

		t.Logf("Test format: string = %s, formatted = %s", s, dsn.Format())

		dsn.SetFileName("file:/dev/null")
		t.Logf("Test set file name: formatted = %s", dsn.Format())

		dsn.AddParam("key", "value")
		t.Logf("Test set add param: formatted = %s", dsn.Format())
	}
}
