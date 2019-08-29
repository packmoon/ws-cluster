package database

import (
	"fmt"
	"log"

	// just init
	_ "github.com/go-sql-driver/mysql"
	"github.com/go-xorm/xorm"
	"xorm.io/core"
)

var (
	// ErrInsertFail data insert affected zeo
	ErrInsertFail = fmt.Errorf("data insert fail")
)

// MysqMessageStore mysql message store
type MysqMessageStore struct {
	engine *xorm.Engine
}

// NewMysqlMessageStore new a MysqMessageStore
func NewMysqlMessageStore(engine *xorm.Engine) *MysqMessageStore {
	if engine == nil {
		return &MysqMessageStore{}
	}
	err := engine.Sync2(new(ChatMsg))
	if err != nil {
		log.Println(err)
	}

	return &MysqMessageStore{
		engine: engine,
	}
}

// SaveChatMsg save message to mysql
func (s *MysqMessageStore) SaveChatMsg(msgs []*ChatMsg) error {
	if s.engine == nil {
		return nil
	}
	_, err := s.engine.Insert(msgs)
	if err != nil {
		return err
	}
	return nil
}

// SaveGroupMsg SaveGroupMsg
func (s *MysqMessageStore) SaveGroupMsg(msgs []*GroupMsg) error {
	if s.engine == nil {
		return nil
	}
	_, err := s.engine.Insert(msgs)
	if err != nil {
		return err
	}
	return nil
}

// InitDb init database
func InitDb(ip string, port int, user, pwd, dbname string) *xorm.Engine {
	url := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8&parseTime=True&loc=Local", user, pwd, ip, port, dbname)
	engine, err := xorm.NewEngine("mysql", url)
	if err != nil {
		log.Println(err)
		return nil
	}

	// engine.ShowSQL(true)

	tbMapper := core.NewPrefixMapper(core.SnakeMapper{}, "t_")
	engine.SetTableMapper(tbMapper)

	engine.SetColumnMapper(core.SnakeMapper{})

	return engine
}
