FROM haproxy

RUN apt-get update -y
RUN apt-get install python python-dev python-pip -y
RUN pip install kazoo gevent

COPY haproxy.cfg /usr/local/etc/haproxy/haproxy.cfg
COPY config_updater.py ./
COPY cmd.sh ./

CMD ./cmd.sh
