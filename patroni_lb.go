package main

import (
	"encoding/json"
	"fmt"
	"github.com/samuel/go-zookeeper/zk"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const configPath = "haproxy.cfg"
const configTemplate = `global
	maxconn 100

defaults
	log	global
	mode	tcp
	retries 2
	timeout client 30m
	timeout connect 4s
	timeout server 30m
	timeout check 5s

frontend ft_postgres_master
	bind *:5432
	default_backend bk_postgres_master

backend bk_postgres_master
	option pgsql-check user admin 
%v

frontend ft_postgres_slaves
	bind *:5433
	default_backend bk_postgres_slaves

backend bk_postgres_slaves
	option pgsql-check user admin 
%v

listen stats
	bind 0.0.0.0:9090
	balance
	mode http
	stats enable
	timeout client 5000
	timeout connect 4000
	timeout server 30000
	stats realm Haproxy\ Statistics
	stats uri /`

type Node struct {
	Path     string
	Name     string
	Value    string
	Children map[string]*Node
}

func WatchTree(conn *zk.Conn, node *Node, chChan chan<- bool, stop <-chan bool) {
	value, _, getChan, err := conn.GetW(node.Path)
	if err != nil {
		log.Fatalf("%v: GetW error:%v\n", node.Path, err)
	} else {
		node.Value = string(value)
	}
	childrenNames, _, childrenChan, err := conn.ChildrenW(node.Path)
	if err != nil {
		log.Fatalf("%v: ChildrenW error:%v\n", node.Path, err)
	} else {
		node.Value = string(value)
	}
	for _, childName := range childrenNames {
		childNode := &Node{
			Path:     node.Path + "/" + childName,
			Name:     childName,
			Children: make(map[string]*Node),
		}
		node.Children[childName] = childNode
		WatchTree(conn, childNode, chChan, stop)
	}
	go func() {
		select {
		case <-getChan:
			chChan <- true
			// log.Println("New value:", node.Path)
		case <-childrenChan:
			chChan <- true
			// log.Println("Children changed", node.Path)
		case <-stop:
			return
		}
	}()
}

func getEnvOrDie(envName string) string {
	envVal := os.Getenv(envName)
	if envVal == "" {
		log.Fatalln(envName, "environment variable is not specified. Exit")
	}
	return envVal
}

func PrintTree(node *Node, i string) {
	log.Printf("%v-%v: %v\n", i, node.Name, node.Value)
	for _, child := range node.Children {
		PrintTree(child, i+"\t")
	}
}

type Backend struct {
	Id        string
	Host      string
	CheckPort string
	Running   bool
	Master    bool
}

type ZkRecord struct {
	ConnUrl string `json:"conn_url"`
	ApiURL  string `json:"api_url"`
	State   string `json:"state"`
	Role    string `json:"role"`
}

func dataToBackend(data string) *Backend {
	zkRecord := &ZkRecord{}
	if err := json.Unmarshal([]byte(data), zkRecord); err != nil {
		log.Fatalln("Cannot parse params json:", err)
	}
	backend := &Backend{}
	backend.Running = zkRecord.State == "running"
	backend.Master = zkRecord.Role == "master"
	connUrl, err := url.Parse(zkRecord.ConnUrl)
	if err != nil {
		log.Fatalln("Cannot parse connUrl:", err)
	}
	backend.Host = connUrl.Host
	backend.Id = "postgresql_" + strings.Replace(backend.Host, ":", "_", -1)
	apiUrl, err := url.Parse(zkRecord.ApiURL)
	if err != nil {
		log.Fatalln("Cannot parse apiUrl:", err)
	}
	backend.CheckPort = strings.Split(apiUrl.Host, ":")[1]
	return backend
}

func nodeToConig(node *Node) string {
	membersNode, ok := node.Children["members"]
	if !ok {
		log.Fatalln("ZK tree is in invalid state, no 'members' node")
	}
	members := membersNode.Children
	var master string
	slaves := make([]string, 0)
	for _, member := range members {
		backend := dataToBackend(member.Value)
		if backend.Master {
			master = BackendToString(backend)
		} else if backend.Running {
			slaves = append(slaves, BackendToString(backend)+"\n")
			sort.Strings(slaves)
		}

	}
	return fmt.Sprintf(configTemplate, master, strings.Join(slaves, ""))
}

func BackendToString(backend *Backend) string {
	return fmt.Sprintf("\tserver %v %v maxconn 100 check",
		backend.Id, backend.Host)
}

func main() {
	zkHost := getEnvOrDie("ZOOKEEPER_HOST")
	zkPort := getEnvOrDie("ZOOKEEPER_PORT")
	scope := getEnvOrDie("PATRONI_SCOPE")
	log.Println("Connecting to ZK")
	conn, _, err := zk.Connect([]string{zkHost + ":" + zkPort}, time.Second*30)
	if err != nil {
		log.Fatalln("Cannot connect to ZK:", err)
	}
	defer conn.Close()

	log.Println("Waiting unitl leader node appeared...")
	for {
		exists, _, eChan, err := conn.ExistsW("/service/" + scope + "/leader")
		if exists {
			break
		}
		if err != nil {
			log.Fatalf("ExistsW error:%v\n", err)
		}
		<-eChan
	}
	log.Println("Leader node appeared. Start serving")
	var pid int
	chChan := make(chan bool)
	var currentConfig string
	for {
		node := &Node{
			Path:     "/service/" + scope,
			Name:     scope,
			Children: make(map[string]*Node),
		}
		stop := make(chan bool)
		WatchTree(conn, node, chChan, stop)
		// PrintTree(node, "")
		newConfig := nodeToConig(node)
		if newConfig != currentConfig {
			log.Println("---==== New HA-Proxy config ====---")
			log.Println(newConfig)
			log.Println("---=============================---")
			ioutil.WriteFile(configPath, []byte(newConfig), 0777)

			// Start HA-Proxy
			if pid == 0 {
				cmd := exec.Command("haproxy")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				err := cmd.Start()
				if err != nil {
					log.Fatalln("Cannot start haproxy:", err)
				}
				pid = cmd.Process.Pid
				log.Println("HA-Proxy started, PID:", pid)
			}

			// Reload config
			cmd := exec.Command("haproxy", "-f", "haproxy.cfg",
				"-p", strconv.Itoa(pid), "-sf", strconv.Itoa(pid))
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Start()
			if err != nil {
				log.Fatal("Cannot reload haproxy:", err)
			}
			log.Println("HA-Proxy reloaded")
			currentConfig = newConfig
		}
		<-chChan
		close(stop)
	}
}
