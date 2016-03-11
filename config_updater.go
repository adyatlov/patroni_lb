package main

import (
	"github.com/samuel/go-zookeeper/zk"
	"log"
	"os"
	"time"
)

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
	bind *:5000
	default_backend bk_postgres_master

backend bk_postgres_master
	option httpchk
	server postgresql_127.0.0.1_5433 127.0.0.1:5433 maxconn 100 check port 8009

frontend ft_postgres_slaves
	bind *:5001
	default_backend bk_postgres_slaves

backend bk_postgres_slaves
	option httpchk
	server postgresql_127.0.0.1_5433 127.0.0.1:5433 maxconn 100 check port 8009`

func getEnvOrDie(envName string) string {
	envVal := os.Getenv(envName)
	if envVal == "" {
		log.Fatalln(envName, "environment variable is not specified. Exit")
	}
	return envVal
}

func watchNodeValue(conn *zk.Conn, path string) <-chan string {
	vChan := make(chan string)
	go func() {
		for {
			data, _, eChan, err := conn.GetW(path)
			if err != nil {
				log.Printf("GetW error %v\n", err)
				continue
			}
			log.Printf("Node %v changed, new value: %v\n", path, string(data))
			vChan <- string(data)
			event := <-eChan
			if event.Err != nil {
				log.Printf("GetW event error %v\n", err)
			}
		}
	}()
	return vChan
}

func watchChidlren(conn *zk.Conn, path string) <-chan map[string]string {
	cChan := make(chan map[string]string)
	go func() {
		for {
			names, _, eChan, err := conn.ChildrenW(path)
			if err != nil {
				log.Printf("ChildrenW error %v\n", err)
				continue
			}
			log.Printf("Children of node %v changed, new set: %v\n",
				path, names)
			children := make(map[string]string)
			for _, name := range names {
				cPath := path + "/" + name
				data, _, err := conn.Get(cPath)
				if err != nil {
					log.Printf("Cannot read from %v: %v", cPath, err)
					continue
				}
				children[name] = string(data)
			}
			cChan <- children
			event := <-eChan
			if event.Err != nil {
				log.Printf("ChildrenW event error %v\n", err)
			}
		}
	}()
	return cChan
}

type config struct {
	// Leader ZK-record ID
	leader string
	// ID -> instnce JSON config
	members map[string]string
}

func main() {
	zkHost := getEnvOrDie("ZK_HOST")
	scope := getEnvOrDie("PATRONI_SCOPE")
	log.Println("Connecting to ZK")
	conn, _, err := zk.Connect([]string{zkHost}, time.Second*30)
	if err != nil {
		log.Fatalln("Cannot connect to ZK:", err)
	}
	defer conn.Close()
	leaderChan := watchNodeValue(conn, "/service/"+scope+"/leader")
	membersChan := watchChidlren(conn, "/service/"+scope+"/members")
	conf := config{}
	for {
		select {
		case conf.leader = <-leaderChan:
			log.Println("Leader changed:", conf.leader)
			log.Println("New leaders config:", conf.members[conf.leader])
		case conf.members = <-membersChan:
			log.Println("Members changed:", conf.members)
		}
	}
}
