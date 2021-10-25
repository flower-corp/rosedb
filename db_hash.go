package rosedb

import (
	"bytes"
	"github.com/roseduan/rosedb/index"
	"sync"
	"time"

	"github.com/roseduan/rosedb/ds/hash"
	"github.com/roseduan/rosedb/storage"
)

// HashIdx hash index.
type HashIdx struct {
	mu      *sync.RWMutex
	indexes *hash.Hash
}

// create a new hash index.
func newHashIdx() *HashIdx {
	return &HashIdx{indexes: hash.New(), mu: new(sync.RWMutex)}
}

// HSet sets field in the hash stored at key to value. If key does not exist, a new key holding a hash is created.
// If field already exists in the hash, it is overwritten.
// Return num of elements in hash of the specified key.
func (db *RoseDB) HSet(key []byte, field []byte, value []byte) (res int, err error) {
	if err = db.checkKeyValue(key, value); err != nil {
		return
	}

	// In KeyValueMemMode, value stored in memory.
	// If the existed value is the same as the set value, nothing will be done.
	if db.config.IdxMode == KeyValueMemMode {
		oldVal := db.HGet(key, field)
		if bytes.Compare(oldVal, value) == 0 {
			return
		}
	}

	db.hashIndex.mu.Lock()
	defer db.hashIndex.mu.Unlock()

	e := storage.NewEntry(key, value, field, Hash, HashHSet)
	if err = db.store(e); err != nil {
		return
	}

	res = db.setHashIndexer(e, false)
	return
}

// HSetNx Sets field in the hash stored at key to value, only if field does not yet exist.
// If key does not exist, a new key holding a hash is created. If field already exists, this operation has no effect.
// Return if the operation is successful.
func (db *RoseDB) HSetNx(key, field, value []byte) (res int, err error) {
	if err = db.checkKeyValue(key, value); err != nil {
		return
	}

	db.hashIndex.mu.Lock()
	defer db.hashIndex.mu.Unlock()
	e := storage.NewEntry(key, value, field, Hash, HashHSet)

	if res = db.setHashIndexer(e, true); res == 1 {
		// db store
		if err = db.store(e); err != nil {
			return
		}
	}
	return
}

func (db *RoseDB) setHashIndexer(e *storage.Entry, onlyNotExist bool) int {
	activeFile, err := db.getActiveFile(Hash)
	if err != nil {
		return 0
	}

	idx := &index.Indexer{
		Meta: &storage.Meta{
			Key:   e.Meta.Key,
			Extra: e.Meta.Extra,
		},
		FileId: activeFile.Id,
		Offset: activeFile.Offset - int64(e.Size()),
	}

	// in KeyValueMemMode, both key and value will store in memory.
	if db.config.IdxMode == KeyValueMemMode {
		idx.Meta.Value = e.Meta.Value
	}
	return db.hashIndex.indexes.HSetIndexer(idx, onlyNotExist)
}

// HGet returns the value associated with field in the hash stored at key.
func (db *RoseDB) HGet(key, field []byte) []byte {
	if err := db.checkKeyValue(key, nil); err != nil {
		return nil
	}

	db.hashIndex.mu.RLock()
	defer db.hashIndex.mu.RUnlock()

	if db.checkExpired(key, Hash) {
		return nil
	}
	idx := db.hashIndex.indexes.HGet(string(key), string(field))

	//In KeyValueMemMode, value stored in memory.
	if db.config.IdxMode == KeyValueMemMode {
		return idx.Meta.Value
	}

	// In KeyOnlyMemMode, value not in memory.
	// Read value from db file according to idx.
	if db.config.IdxMode == KeyOnlyMemMode {
		return db.readValFromDbFile(idx, Hash)
	}
	return nil
}

// HGetAll returns all fields and values of the hash stored at key.
// In the returned value, every field name is followed by its value, so the length of the reply is twice the size of the hash.
func (db *RoseDB) HGetAll(key []byte) [][]byte {
	if err := db.checkKeyValue(key, nil); err != nil {
		return nil
	}

	db.hashIndex.mu.RLock()
	defer db.hashIndex.mu.RUnlock()

	if db.checkExpired(key, Hash) {
		return nil
	}

	idxSli := db.hashIndex.indexes.HGetAll(string(key))

	//In KeyValueMemMode, value stored in memory.
	if db.config.IdxMode == KeyValueMemMode {
		return db.getValSliByIndexers(idxSli, func(idx *index.Indexer) []byte {
			return idx.Meta.Value
		})
	}

	// In KeyOnlyMemMode, value not in memory.
	// Read value from db file according to idx.
	if db.config.IdxMode == KeyOnlyMemMode {
		return db.getValSliByIndexers(idxSli, func(idx *index.Indexer) []byte {
			return db.readValFromDbFile(idx, Hash)
		})
	}
	return nil
}

func (db *RoseDB) getValSliByIndexers(idxSli []*index.Indexer, f func(idx *index.Indexer) []byte) [][]byte {
	ret := make([][]byte, 0, len(idxSli))
	for _, idx := range idxSli {
		ret = append(ret, f(idx))
	}
	return ret
}

// HMSet set multiple hash fields to multiple values.
// Example: key, field1, value1, field2, value2 ... -> key: {field1 -> value1, field2 -> value2 ...}
func (db *RoseDB) HMSet(key []byte, values ...[]byte) error {
	if len(values)%2 != 0 {
		return ErrWrongNumberOfArgs
	}

	if err := db.checkKeyValue(key, nil); err != nil {
		return err
	}

	fields := make([][]byte, 0)
	for i := 0; i < len(values); i += 2 {
		fields = append(fields, values[i])
	}

	existVals := db.HMGet(key, fields...)
	// field1 value1 field2 value2 ...
	insertVals := make([][]byte, 0)

	if existVals == nil {
		// existVals means key expired
		insertVals = values
	} else {
		for i := 0; i < len(existVals); i++ {
			// If the existed value is the same as the set value, pass this field and value
			if bytes.Compare(values[i*2+1], existVals[i]) != 0 {
				insertVals = append(insertVals, fields[i], values[i*2+1])
			}
		}
	}

	// check all fields and values.
	if err := db.checkKeyValue(key, insertVals...); err != nil {
		return err
	}

	db.hashIndex.mu.Lock()
	defer db.hashIndex.mu.Unlock()

	for i := 0; i < len(insertVals); i += 2 {
		e := storage.NewEntry(key, insertVals[i+1], insertVals[i], Hash, HashHSet)
		if err := db.store(e); err != nil {
			return err
		}

		db.setHashIndexer(e, false)
	}

	return nil
}

// HMGet get the values of all the given hash fields
func (db *RoseDB) HMGet(key []byte, fields ...[]byte) [][]byte {
	if err := db.checkKeyValue(key, nil); err != nil {
		return nil
	}

	db.hashIndex.mu.RLock()
	defer db.hashIndex.mu.RUnlock()

	if db.checkExpired(key, Hash) {
		return nil
	}

	idxSli := make([]*index.Indexer, 0)

	for _, field := range fields {
		value := db.hashIndex.indexes.HGet(string(key), string(field))
		idxSli = append(idxSli, value)
	}

	if db.config.IdxMode == KeyValueMemMode {
		return db.getValSliByIndexers(idxSli, func(idx *index.Indexer) []byte {
			return idx.Meta.Value
		})
	}

	if db.config.IdxMode == KeyOnlyMemMode {
		return db.getValSliByIndexers(idxSli, func(idx *index.Indexer) []byte {
			return db.readValFromDbFile(idx, Hash)
		})
	}

	return nil
}

// HDel removes the specified fields from the hash stored at key.
// Specified fields that do not exist within this hash are ignored.
// If key does not exist, it is treated as an empty hash and this command returns false.
func (db *RoseDB) HDel(key []byte, field ...[]byte) (res int, err error) {
	if err = db.checkKeyValue(key, nil); err != nil {
		return
	}

	if field == nil || len(field) == 0 {
		return
	}

	db.hashIndex.mu.Lock()
	defer db.hashIndex.mu.Unlock()

	for _, f := range field {
		if ok := db.hashIndex.indexes.HDel(string(key), string(f)); ok == 1 {
			e := storage.NewEntry(key, nil, f, Hash, HashHDel)
			if err = db.store(e); err != nil {
				return
			}
			res++
		}
	}
	return
}

// HKeyExists returns if the key is existed in hash.
func (db *RoseDB) HKeyExists(key []byte) (ok bool) {
	if err := db.checkKeyValue(key, nil); err != nil {
		return
	}

	db.hashIndex.mu.RLock()
	defer db.hashIndex.mu.RUnlock()

	if db.checkExpired(key, Hash) {
		return
	}
	return db.hashIndex.indexes.HKeyExists(string(key))
}

// HExists returns if field is an existing field in the hash stored at key.
func (db *RoseDB) HExists(key, field []byte) (ok bool) {
	if err := db.checkKeyValue(key, nil); err != nil {
		return
	}

	db.hashIndex.mu.RLock()
	defer db.hashIndex.mu.RUnlock()

	if db.checkExpired(key, Hash) {
		return
	}

	return db.hashIndex.indexes.HExists(string(key), string(field))
}

// HLen returns the number of fields contained in the hash stored at key.
func (db *RoseDB) HLen(key []byte) int {
	if err := db.checkKeyValue(key, nil); err != nil {
		return 0
	}

	db.hashIndex.mu.RLock()
	defer db.hashIndex.mu.RUnlock()

	if db.checkExpired(key, Hash) {
		return 0
	}

	return db.hashIndex.indexes.HLen(string(key))
}

// HKeys returns all field names in the hash stored at key.
func (db *RoseDB) HKeys(key []byte) (val []string) {
	if err := db.checkKeyValue(key, nil); err != nil {
		return
	}

	db.hashIndex.mu.RLock()
	defer db.hashIndex.mu.RUnlock()

	if db.checkExpired(key, Hash) {
		return nil
	}

	return db.hashIndex.indexes.HKeys(string(key))
}

// HVals returns all values in the hash stored at key.
func (db *RoseDB) HVals(key []byte) (val [][]byte) {
	if err := db.checkKeyValue(key, nil); err != nil {
		return
	}

	db.hashIndex.mu.RLock()
	defer db.hashIndex.mu.RUnlock()

	if db.checkExpired(key, Hash) {
		return nil
	}

	idxSli := db.hashIndex.indexes.HVals(string(key))

	if db.config.IdxMode == KeyValueMemMode {
		return db.getValSliByIndexers(idxSli, func(idx *index.Indexer) []byte {
			return idx.Meta.Value
		})
	}

	if db.config.IdxMode == KeyOnlyMemMode {
		return db.getValSliByIndexers(idxSli, func(idx *index.Indexer) []byte {
			return db.readValFromDbFile(idx, Hash)
		})
	}

	return nil
}

// HClear clear the key in hash.
func (db *RoseDB) HClear(key []byte) (err error) {
	if err = db.checkKeyValue(key, nil); err != nil {
		return
	}

	if !db.HKeyExists(key) {
		return ErrKeyNotExist
	}

	db.hashIndex.mu.Lock()
	defer db.hashIndex.mu.Unlock()

	e := storage.NewEntryNoExtra(key, nil, Hash, HashHClear)
	if err := db.store(e); err != nil {
		return err
	}

	db.hashIndex.indexes.HClear(string(key))
	delete(db.expires[Hash], string(key))
	return
}

// HExpire set expired time for a hash key.
func (db *RoseDB) HExpire(key []byte, duration int64) (err error) {
	if duration <= 0 {
		return ErrInvalidTTL
	}
	if err = db.checkKeyValue(key, nil); err != nil {
		return
	}
	if !db.HKeyExists(key) {
		return ErrKeyNotExist
	}

	db.hashIndex.mu.Lock()
	defer db.hashIndex.mu.Unlock()

	deadline := time.Now().Unix() + duration
	e := storage.NewEntryWithExpire(key, nil, deadline, Hash, HashHExpire)
	if err := db.store(e); err != nil {
		return err
	}

	db.expires[Hash][string(key)] = deadline
	return
}

// HTTL return time to live for the key.
func (db *RoseDB) HTTL(key []byte) (ttl int64) {
	db.hashIndex.mu.RLock()
	defer db.hashIndex.mu.RUnlock()

	if db.checkExpired(key, Hash) {
		return
	}

	deadline, exist := db.expires[Hash][string(key)]
	if !exist {
		return
	}
	return deadline - time.Now().Unix()
}
