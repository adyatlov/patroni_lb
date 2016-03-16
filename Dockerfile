FROM haproxy
COPY patroni_lb ./
CMD ./patroni_lb
