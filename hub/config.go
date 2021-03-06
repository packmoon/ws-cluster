package hub

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/segmentio/ksuid"
	"github.com/ws-cluster/database"
)

const (
	// defaultConfigName  = "conf.ini"
	defaultIDName      = "id.lock"
	defaultMessageName = "message.log"
)

const (
	// Time allowed to write a message to the peer.
	defaultWriteWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	defaultPongWait = 20 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	defaultPingPeriod = 10 * time.Second

	// Maximum message size allowed from peer.
	defaultMaxMessageSize = 2048
)

var (
	// configDir = "./"
	defaultDataDir         = "./data"
	defaultDbDriver        = "mysql"
	defaultWebsocketScheme = "ws"
	defaultListenIP        = "0.0.0.0"
	defaultListenPort      = 8380
	defaultGroupBufferSize = 10
	// defaultConfigFile   = filepath.Join(configDir, defaultConfigName)
)

type serverConfig struct {
	ID                 string `description:"server logic addr"`
	AcceptDomains      []int
	ListenHost         string
	AdvertiseClientURL *url.URL
	AdvertiseServerURL *url.URL
	ClientToken        string
	ServerToken        string
	ClusterSeedURL     string
	Origins            string
	MessageFile        string
	GroupBufferSize    int
}

type peerConfig struct {
	MaxMessageSize int
	WriteWait      time.Duration
	PongWait       time.Duration
	PingPeriod     time.Duration
}

type databaseConfig struct {
	DbDriver string
	DbSource string
}

// Config 系统配置信息，包括 redis 配置， mongodb 配置
type Config struct {
	// server
	sc serverConfig
	dc *databaseConfig
	//client peer config
	cpc     peerConfig
	dataDir string
	// Cache        Cache
	ms database.MessageStore
}

// LoadConfig LoadConfig
func LoadConfig() (*Config, error) {
	var conf Config

	conf.sc = serverConfig{}
	flag.StringVar(&conf.sc.ID, "server-id", "", "server id")
	flag.StringVar(&conf.sc.ListenHost, "listen-host", fmt.Sprintf("%v:%v", defaultListenIP, defaultListenPort), "listen host,format ip:port")
	flag.StringVar(&conf.sc.Origins, "origins", "*", "allowed origins from client")
	flag.StringVar(&conf.sc.ClientToken, "client-token", ksuid.New().String(), "token for client")
	flag.StringVar(&conf.sc.ServerToken, "server-token", ksuid.New().String(), "token for server")
	flag.StringVar(&conf.sc.ClusterSeedURL, "cluster-seed-url", "", "request a server for downloading a list of servers")
	flag.IntVar(&conf.sc.GroupBufferSize, "group-buffer-size", defaultGroupBufferSize, "group channal size of relying message")

	var clientURL, serverURL string
	flag.StringVar(&clientURL, "advertise-client-url", "", "the url is to listen on for client traffic")
	flag.StringVar(&serverURL, "advertise-server-url", "", "use for server connecting")

	conf.cpc = peerConfig{}
	flag.IntVar(&conf.cpc.MaxMessageSize, "client-max-msg-size", defaultMaxMessageSize, "Maximum message size allowed from client.")
	flag.DurationVar(&conf.cpc.WriteWait, "client-write-wait", defaultWriteWait, "Time allowed to write a message to the client")
	flag.DurationVar(&conf.cpc.PingPeriod, "client-ping-period", defaultWriteWait, "Send pings to client with this period. Must be less than pongWait")
	flag.DurationVar(&conf.cpc.PongWait, "client-pong-wait", defaultWriteWait, "Time allowed to read the next pong message from the client")

	dbsource := *flag.String("db-source", "", "database source, just support mysql,eg: user:password@tcp(ip:port)/dbname")

	// datadir
	flag.StringVar(&conf.dataDir, "data-dir", defaultDataDir, "data directory")

	flag.Usage = func() {
		fmt.Println("Usage of wscluster:")
		flag.PrintDefaults()
	}

	flag.Parse()

	listenPort := strings.Split(conf.sc.ListenHost, ":")[1]
	var err error
	if clientURL != "" {
		conf.sc.AdvertiseClientURL, err = url.Parse(clientURL)
		if err != nil {
			return nil, err
		}
		log.Println("-advertise-client-url", conf.sc.AdvertiseClientURL.String())
	} else {
		conf.sc.AdvertiseClientURL = &url.URL{Scheme: defaultWebsocketScheme, Host: fmt.Sprintf("%v:%v", GetOutboundIP().String(), listenPort)}
	}

	if serverURL != "" {
		conf.sc.AdvertiseServerURL, err = url.Parse(serverURL)
		if err != nil {
			return nil, err
		}
		log.Println("-advertise-server-url", conf.sc.AdvertiseServerURL.String())
	} else {
		conf.sc.AdvertiseServerURL = &url.URL{Scheme: defaultWebsocketScheme, Host: fmt.Sprintf("%v:%v", GetOutboundIP().String(), listenPort)}
	}

	conf.sc.MessageFile = filepath.Join(conf.dataDir, defaultMessageName)
	if _, err := os.Stat(conf.dataDir); err != nil {
		err = os.MkdirAll(conf.dataDir, os.ModePerm)
		if err != nil {
			fmt.Println(err)
			return nil, err
		}
	}

	if conf.sc.ID == "" {
		conf.sc.ID = fmt.Sprintf("%d", time.Now().Unix())
	}

	if dbsource != "" {
		conf.dc = new(databaseConfig)
		flag.StringVar(&conf.dc.DbDriver, "db-driver", defaultDbDriver, "database dirver, just support mysql")
		conf.dc.DbSource = dbsource

		log.Println("-db-source", conf.dc.DbSource)
	}

	// if err != nil {
	// 	return nil, err
	// }
	log.Println("-client-token", conf.sc.ClientToken)
	log.Println("-server-token", conf.sc.ServerToken)

	return &conf, nil
}

// BuildServerID build a serverID
func BuildServerID(dataDir string) (string, error) {
	defaultIDConfigFile := filepath.Join(dataDir, defaultIDName)
	// deal server id
	_, err := os.Stat(defaultIDConfigFile)
	if err != nil {
		sid := fmt.Sprintf("%d", time.Now().Unix())
		ioutil.WriteFile(defaultIDConfigFile, []byte(sid), 0644)
	}
	fb, err := ioutil.ReadFile(defaultIDConfigFile)
	if err != nil {
		return "", err
	}
	return string(fb), nil
}

//GetOutboundIP Get preferred outbound ip of this machine
func GetOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Println(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP
}
