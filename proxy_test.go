// +build integration

package dvara

import (
	"os"
	"testing"

	"github.com/facebookgo/ensure"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var harness *ReplicaSetHarness

func TestMain(m *testing.M) {
	harness = NewReplicaSetHarness(3, nil)
	code := m.Run()
	harness.Stop()
	os.Exit(code)
}

func withHarness(t *testing.T, testFunc func(harness *ReplicaSetHarness)) {
	harness.T = t
	testFunc(harness)
}

func TestParallelInsertWithUniqueIndex(t *testing.T) {
	withHarness(t, func(harness *ReplicaSetHarness) {
		limit := 20000
		c := make(chan int, limit)
		for i := 0; i < 3; i++ {
			go inserter(harness.ProxySession(), c, limit)
		}
		set := make(map[int]bool)
		for k := range c {
			if set[k] {
				t.Fatal("Double write on same value")
			}
			set[k] = true
			if len(set) == limit {
				break
			}
		}
	})
}

func inserter(s *mgo.Session, channel chan int, limit int) {
	defer s.Close()
	c := s.DB("test").C("test")
	c.EnsureIndex(mgo.Index{Key: []string{"phoneNum"}, Unique: true})
	for i := 1; i <= limit; i++ {
		if err := c.Insert(bson.M{"phoneNum": i}); err == nil {
			channel <- i
		}
	}
}
func TestSimpleCRUD(t *testing.T) {
	withHarness(t, func(harness *ReplicaSetHarness) {
		session := harness.ProxySession()
		defer session.Close()
		collection := session.DB("test").C("coll1")
		data := map[string]interface{}{
			"_id":  1,
			"name": "abc",
		}
		err := collection.Insert(data)
		if err != nil {
			t.Fatal("insertion error", err)
		}
		n, err := collection.Count()
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("expecting 1 got %d", n)
		}
		result := make(map[string]interface{})
		collection.Find(bson.M{"_id": 1}).One(&result)
		if result["name"] != "abc" {
			t.Fatal("expecting name abc got", result)
		}
		err = collection.DropCollection()
		if err != nil {
			t.Fatal(err)
		}
	})
}

// inserting data with same id field twice should fail
func TestIDConstraint(t *testing.T) {
	withHarness(t, func(harness *ReplicaSetHarness) {
		session := harness.ProxySession()
		defer session.Close()
		collection := session.DB("test").C("coll1")
		data := map[string]interface{}{
			"_id":  1,
			"name": "abc",
		}
		err := collection.Insert(data)
		if err != nil {
			t.Fatal("insertion error", err)
		}
		err = collection.Insert(data)
		if err == nil {
			t.Fatal("insertion failed on same id without write concern")
		}
	})
}

// inserting data voilating index clause on a separate connection should fail
func TestEnsureIndex(t *testing.T) {
	withHarness(t, func(harness *ReplicaSetHarness) {
		session := harness.ProxySession()
		collection := session.DB("test").C("testensureindex")
		collection.DropIndex("lastname", "firstname")
		index := mgo.Index{
			Key:        []string{"lastname", "firstname"},
			Unique:     true,
			DropDups:   true,
			Background: true, // See notes.
			Sparse:     true,
		}
		err := collection.EnsureIndex(index)
		ensure.Nil(t, err)
		err = collection.Insert(
			map[string]string{
				"firstname": "harvey",
				"lastname":  "dent",
			},
		)
		if err != nil {
			t.Fatal("insertion error", err)
		}
		session.Close()
		session = harness.ProxySession()
		defer session.Close()
		collection = session.DB("test").C("testensureindex")
		err = collection.Insert(
			map[string]string{
				"firstname": "harvey",
				"lastname":  "dent",
			},
		)
		ensure.NotNil(t, err)
	})
}

// inserting same data after dropping an index should work
func TestDropIndex(t *testing.T) {
	withHarness(t, func(harness *ReplicaSetHarness) {
		session := harness.ProxySession()
		collection := session.DB("test").C("testdropindex")
		collection.DropIndex("lastname", "firstname")
		index := mgo.Index{
			Key:        []string{"lastname", "firstname"},
			Unique:     true,
			DropDups:   true,
			Background: true, // See notes.
			Sparse:     true,
		}
		err := collection.EnsureIndex(index)
		if err != nil {
			t.Fatal("ensure index call failed")
		}
		err = collection.Insert(
			map[string]string{
				"firstname": "harvey",
				"lastname":  "dent",
			},
		)
		if err != nil {
			t.Fatal("insertion error", err)
		}
		collection.DropIndex("lastname", "firstname")
		session.Close()
		session = harness.ProxySession()
		defer session.Close()
		collection = session.DB("test").C("testdropindex")
		err = collection.Insert(
			map[string]string{
				"firstname": "harvey",
				"lastname":  "dent",
			},
		)
		if err != nil {
			t.Fatal("drop index did not work")
		}
	})
}

func TestRemoval(t *testing.T) {
	withHarness(t, func(harness *ReplicaSetHarness) {
		session := harness.ProxySession()
		defer session.Close()
		collection := session.DB("test").C("testremoval")
		if err := collection.Insert(bson.M{"S": "hello", "I": 24}); err != nil {
			t.Fatal(err)
		}
		if err := collection.Remove(bson.M{"S": "hello", "I": 24}); err != nil {
			t.Fatal(err)
		}
		var res []interface{}
		collection.Find(bson.M{"S": "hello", "I": 24}).All(&res)
		if res != nil {
			t.Fatal("found object after delete", res)
		}
		if err := collection.Remove(bson.M{"S": "hello", "I": 24}); err == nil {
			t.Fatal("removing nonexistant document should error")
		}
	})
}

func TestUpdate(t *testing.T) {
	withHarness(t, func(harness *ReplicaSetHarness) {
		session := harness.ProxySession()
		defer session.Close()
		collection := session.DB("test").C("testupdate")
		if err := collection.Insert(bson.M{"_id": "1234", "name": "Alfred"}); err != nil {
			t.Fatal(err)
		}
		var result map[string]interface{}
		collection.Find(nil).One(&result)
		if result["name"] != "Alfred" {
			t.Fatal("insert failed")
		}
		if err := collection.Update(bson.M{"_id": "1234"}, bson.M{"name": "Jeeves"}); err != nil {
			t.Fatal("update failed with", err)
		}
		collection.Find(nil).One(&result)
		if result["name"] != "Jeeves" {
			t.Fatal("update failed")
		}
		if err := collection.Update(bson.M{"_id": "00000"}, bson.M{"name": "Jeeves"}); err == nil {
			t.Fatal("update failed")
		}
	})
}

func TestStopChattyClient(t *testing.T) {
	withHarness(t, func(harness *ReplicaSetHarness) {
		session := harness.ProxySession()
		defer session.Close()
		fin := make(chan struct{})
		go func() {
			collection := session.DB("test").C("coll1")
			i := 0
			for {
				select {
				default:
					collection.Insert(bson.M{"value": i})
					i++
				case <-fin:
					return
				}
			}
		}()
		close(fin)
	})
}

func TestStopIdleClient(t *testing.T) {
	withHarness(t, func(harness *ReplicaSetHarness) {
		session := harness.ProxySession()
		defer session.Close()
		if err := session.DB("test").C("col").Insert(bson.M{"v": 1}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestZeroMaxConnections(t *testing.T) {
	withHarness(t, func(harness *ReplicaSetHarness) {
		p := &Proxy{ReplicaSet: &ReplicaSet{}}
		err := p.Start()
		if err != errZeroMaxConnections {
			t.Fatal("did not get expected error")
		}
	})
}

func benchmarkInsertRead(b *testing.B, session *mgo.Session) {
	defer session.Close()
	col := session.DB("test").C("col")
	col.EnsureIndex(mgo.Index{Key: []string{"answer"}, Unique: true})
	insertDocs := bson.D{bson.DocElem{Name: "answer"}}
	inserted := bson.M{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		insertDocs[0].Value = i
		if err := col.Insert(insertDocs); err != nil {
			b.Fatal(err)
		}
		if err := col.Find(insertDocs).One(inserted); err != nil {
			b.Fatal(err)
		}
		if _, ok := inserted["_id"]; !ok {
			b.Fatalf("no _id found: %+v", inserted)
		}
	}
}

func BenchmarkInsertReadProxy(b *testing.B) {
	p := NewReplicaSetHarness(3, b)
	benchmarkInsertRead(b, p.ProxySession())
}

func BenchmarkInsertReadDirect(b *testing.B) {
	p := NewReplicaSetHarness(3, b)
	benchmarkInsertRead(b, p.RealSession())
}
