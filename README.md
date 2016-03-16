# patroni_lb
Load balancer for Zalando Patroni

How to use:
1. Edit patroni_lb.json if needed 
1. Deploy patroni_lb on Marathon: dcos marathon app add patroni_lb.json 

How to build:
1. Build LB: GOOS=linux GOARCH=amd64 go build config_updater.go
1. Build image: docker build -t <image/name> .
1. Push the image: docker push <image/name>
