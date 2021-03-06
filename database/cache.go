package database

// ClientCache 定义了 client 缓存操作接口
type ClientCache interface {
	AddClient(client *Client) error
	DelClient(ID string) (int, error)
	GetClient(ID string) (*Client, error)
}

// ServerCache 定义了服务器列表操作方法
type ServerCache interface {
	SetServer(server *Server) error
	GetServer(ID uint64) (*Server, error)
	DelServer(ID uint64) error
	GetServers() ([]Server, error)
	Clean() error
}

// GroupCache GroupCache
type GroupCache interface {
	Join(group string, clientID string)
	Leave(group string, clientID string)
	JoinMany(clientID string, group []string)
	LeaveMany(clientID string, group []string)
	GetGroupMembers(group string) []string
	GetGroups() []string
}
