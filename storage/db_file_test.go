package storage

import (
	"log"
	"os"
	"testing"
)

const (
	path1            = "/tmp/rosedb"
	fileID1          = 0
	path2            = "/tmp/rosedb"
	fileID2          = 1
	defaultBlockSize = 8 * 1024 * 1024
)

func init() {
	os.MkdirAll(path1, os.ModePerm)
	_, err := os.OpenFile("/tmp/rosedb/000000000.data", os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		log.Println("create file err. ", err)
	}
	os.OpenFile("/tmp/rosedb/000000001.data", os.O_CREATE|os.O_RDWR, os.ModePerm)
}

func TestNewDBFile(t *testing.T) {

	newOne := func(method FileRWMethod) {
		_, err := NewDBFile(path1, fileID1, method, defaultBlockSize)
		if err != nil {
			t.Error("new db file error ", err)
		}
	}

	t.Run("new db file file io", func(t *testing.T) {
		newOne(FileIO)
	})

	t.Run("new db file mmap", func(t *testing.T) {
		newOne(MMap)
	})
}

func TestDBFile_Sync(t *testing.T) {
	df, err := NewDBFile(path1, fileID1, FileIO, defaultBlockSize)
	if err != nil {
		t.Error(err)
	}
	df.Sync()
}

func TestDBFile_Close(t *testing.T) {
	df, err := NewDBFile(path1, fileID1, FileIO, defaultBlockSize)
	if err != nil {
		t.Error(err)
	}
	df.Close(true)
}

func TestBuild(t *testing.T) {
	path := "/tmp/rosedb"
	_, _, err := Build(path, FileIO, defaultBlockSize)
	if err != nil {
		log.Fatal(err)
	}
}

func TestDBFile_Write2(t *testing.T) {
	df, err := NewDBFile(path1, fileID1, FileIO, defaultBlockSize)
	if err != nil {
		t.Error(err)
	}

	entry1 := &Entry{
		Meta: &Meta{
			Key:   []byte("test001"),
			Value: []byte("test001"),
		},
	}
	entry1.Meta.KeySize = uint32(len(entry1.Meta.Key))
	entry1.Meta.ValueSize = uint32(len(entry1.Meta.Value))

	entry2 := &Entry{
		Meta: &Meta{
			Key:   []byte("test_key_002"),
			Value: []byte("test_val_002"),
		},
	}

	entry2.Meta.KeySize = uint32(len(entry2.Meta.Key))
	entry2.Meta.ValueSize = uint32(len(entry2.Meta.Value))
	err = df.Write(entry1)
	err = df.Write(entry2)

	defer func() {
		err = df.Close(true)
	}()

	if err != nil {
		t.Error("写入数据错误 : ", err)
	}
}

func TestDBFile_Read2(t *testing.T) {
	//df, _ := NewDBFile(path1, fileID1, FileIO, defaultBlockSize)
	//
	//readEntry := func(offset int64) *Entry {
	//	if e, err := df.Read(offset); err != nil {
	//		t.Error("read db File error ", err)
	//	} else {
	//		return e
	//	}
	//	return nil
	//}
	//
	//_ = readEntry(0)
	//_ = readEntry(30)
	//defer df.Close(false)
}

func TestDBFile_Write(t *testing.T) {

	var df, _ = NewDBFile(path2, fileID2, MMap, defaultBlockSize)

	writeEntry := func(key, value []byte) {
		defer df.Sync()
		e := &Entry{
			Meta: &Meta{
				Key:   key,
				Value: value,
			},
		}
		e.Meta.KeySize = uint32(len(e.Meta.Key))
		e.Meta.ValueSize = uint32(len(e.Meta.Value))

		if err := df.Write(e); err != nil {
			t.Error("数据写入错误", err)
		}
	}
	writeEntry([]byte("mmap_key_001"), []byte("mmap_val_001"))
	writeEntry([]byte("mmap_key_002"), []byte("mmap_val_002"))
}

func TestDBFile_ReadAll(t *testing.T) {
	archFiles, _, err :=  Build("/tmp/rosedb_server", FileIO, 16 * 1024 * 1024)
	if err != nil {
		t.Fatal(err.Error())
	}
	for _, file := range archFiles {
		entries, err := file.ReadAll()
		if err != nil {
			t.Fatal(err.Error())
		}
		if len(entries) != 3 {
			t.Fatal("want 3 entries, got ", len(entries))
		}
	}
}

func TestDBFile_Read(t *testing.T) {
	//readEntry := func(offset int64) {
	//	if e, err := df.Read(offset); err != nil {
	//		t.Error("数据读取失败", err)
	//	} else {
	//		t.Log(string(e.Meta.Key), e.Meta.KeySize, string(e.Meta.Value), e.Meta.ValueSize, e.crc32)
	//	}
	//}
	//readEntry(0)
	//readEntry(40)
}
