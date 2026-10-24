package service

import (
	"encoding/json"
	"net"
)

type netConn = net.Conn
type dialer = net.Dialer

func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }
