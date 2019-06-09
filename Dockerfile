FROM amd64/golang:1.10

COPY . /home

WORKDIR /home/biscuit

RUN apt-get install python2.7

RUN apt-get update && apt-get install -y \
    vim \
    qemu-system \
    && rm -rf /var/lib/apt/lists/*

RUN cd ../src && \
    bash ./make.bash

RUN make clean

ENV GOPATH /home/biscuit

CMD make qemu CPUS=2