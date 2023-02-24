# Dozzle - [dozzle.dev](https://dozzle.dev/)

Dozzle is a small lightweight application with a web based interface to monitor Docker logs. It doesn’t store any log files. It is for live monitoring of your container logs only.

![Image](https://github.com/amir20/dozzle/blob/master/.github/demo.gif?raw=true)

[![Go Report Card](https://goreportcard.com/badge/github.com/amir20/dozzle)](https://goreportcard.com/report/github.com/amir20/dozzle)
[![Docker Pulls](https://img.shields.io/docker/pulls/amir20/dozzle.svg)](https://hub.docker.com/r/amir20/dozzle/)
[![Docker Version](https://img.shields.io/docker/v/amir20/dozzle?sort=semver)](https://hub.docker.com/r/amir20/dozzle/)
![Test](https://github.com/amir20/dozzle/workflows/Test/badge.svg)

## Features

- Intelligent fuzzy search for container names 🤖
- Search logs using regex 🔦
- Small memory footprint 🏎
- Split screen for viewing multiple logs
- Download logs easy
- Live stats with memory and CPU usage
- Authentication with username and password 🚨

Dozzle should work for most. It has been tested with hundreds of containers. However, it doesn't support offline searching. Products like [Loggly](https://www.loggly.com), [Papertrail](https://papertrailapp.com) or [Kibana](https://www.elastic.co/products/kibana) are more suited for full search capabilities.

Dozzle doesn't cost any money and aims to focus on real-time debugging.

## Getting Dozzle

Dozzle is a very small Docker container (4 MB compressed). Pull the latest release from the index:

    $ docker pull amir20/dozzle:latest

## Using Dozzle

The simplest way to use dozzle is to run the docker container. Also, mount the Docker Unix socket with `--volume` to `/var/run/docker.sock`:

    $ docker run --name dozzle -d --volume=/var/run/docker.sock:/var/run/docker.sock -p 8888:8080 amir20/dozzle:latest

Dozzle will be available at [http://localhost:8888/](http://localhost:8888/). You can change `-p 8888:8080` to any port. For example, if you want to view dozzle over port 4040 then you would do `-p 4040:8080`.

### Connecting to remote hosts

Dozzle supports connecting to multiple remote hosts via `tcp://` using TLS or without. Appropriate certs need to be mounted for Dozzle to be able to successfully connect. At this point, `ssh://` is not supported because Dozzle docker image does not ship with any ssh clients.

To configure remote hosts, `--remote-host` or `DOZZLE_REMOTE_HOST` need to provided and the `pem` files need to be mounted to `/cert` directory. The `/cert` directory expects to have `/certs/{ca,cert,key}.pem` or `/certs/{host}/{ca,cert,key}.pem` in case of multiple hosts.

Below are examples of using `--remote-host` via CLI:

    $ docker run -v /var/run/docker.sock:/var/run/docker.sock -v /path/to/certs:/certs -p 8080:8080 amir20/dozzle --remote-host tcp://167.99.1.1:2376

Multiple `--remote-host` flags can be used to specify multiple hosts.

Or to use compose:

    version: "3"
    services:
      dozzle:
        image: amir20/dozzle:latest
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
          - /path/to/certs:/certs
        ports:
          - 8080:8080
        environment:
          DOZZLE_REMOTE_HOST: tcp://167.99.1.1:2376,tcp://167.99.1.2:2376

You need to make sure appropriate certs are provided in `/certs/167.99.1.1/{ca,cert,key}.pem` and `/certs/167.99.1.2/{ca,cert,key}.pem` for both hosts to work.

### With Docker compose

    version: "3"
    services:
      dozzle:
        container_name: dozzle
        image: amir20/dozzle:latest
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
        ports:
          - 9999:8080

### Enabling health check

Dozzle doesn't enable healthcheck by default as it adds extra CPU usage. `healthcheck` can be enabled manually.

    version: "3"
    services:
      dozzle:
        container_name: dozzle
        image: amir20/dozzle:latest
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
        ports:
          - 8080:8080
        environment:
          DOZZLE_LEVEL: trace
        healthcheck:
          test: [ "CMD", "/dozzle", "healthcheck" ]
          interval: 3s
          timeout: 30s
          retries: 5
          start_period: 30s

#### Security

You can control the device Dozzle binds to by passing `--addr` parameter. For example,

    $ docker run --volume=/var/run/docker.sock:/var/run/docker.sock -p 8888:1224 amir20/dozzle:latest --addr localhost:1224

will bind to `localhost` on port `1224`. You can then use a reverse proxy to control who can see dozzle.

If you wish to restrict the containers shown you can pass the `--filter` parameter. For example,

    $ docker run --volume=/var/run/docker.sock:/var/run/docker.sock -p 8888:1224 amir20/dozzle:latest --filter name=foo

this would then only allow you to view containers with a name starting with "foo". You can use other filters like `status` as well, please check the official docker [command line docs](https://docs.docker.com/engine/reference/commandline/ps/#filtering) for available filters. Multiple `--filter` arguments can be provided.

#### Authentication

Dozzle supports a very simple authentication out of the box with just username and password. You should deploy using SSL to keep the credentials safe. See configuration to use `--username` and `--password`. You can also use [docker secrets](https://docs.docker.com/engine/swarm/secrets/) `--usernamefile` and `--passwordfile`.

#### Changing base URL

Dozzle by default mounts to "/". If you want to control the base path you can use the `--base` option. For example, if you want to mount at "/foobar",
then you can override by using `--base /foobar`. See env variables below for using `DOZZLE_BASE` to change this.

    $ docker run --volume=/var/run/docker.sock:/var/run/docker.sock -p 8080:8080 amir20/dozzle:latest --base /foobar

Dozzle will be available at [http://localhost:8080/foobar/](http://localhost:8080/foobar/).

#### Analytics collected

Dozzle collects anonymous user configurations using Google Analytics. Why? Dozzle is an open source project with no funding. As a result, there is no time to do user studies of Dozzle. Analytics is collected to prioritize features and fixes based on how people use Dozzle. This data is completely public and can be viewed live using [ Data Studio dashboard](https://datastudio.google.com/s/naeIu0MiWsY).

If you do not want to be tracked at all, see the `--no-analytics` flag below.

#### Environment variables and configuration

Dozzle follows the [12-factor](https://12factor.net/) model. Configurations can use the CLI flags or environment variables. The table below outlines all supported options and their respective env vars.

| Flag             | Env Variable           | Default |
| ---------------- | ---------------------- | ------- |
| `--addr`         | `DOZZLE_ADDR`          | `:8080` |
| `--base`         | `DOZZLE_BASE`          | `/`     |
| `--hostname`     | `DOZZLE_HOSTNAME`      | `""`    |
| `--level`        | `DOZZLE_LEVEL`         | `info`  |
| `--filter`       | `DOZZLE_FILTER`        | `""`    |
| `--username`     | `DOZZLE_USERNAME`      | `""`    |
| `--password`     | `DOZZLE_PASSWORD`      | `""`    |
| `--usernamefile` | `DOZZLE_USERNAME_FILE` | `""`    |
| `--passwordfile` | `DOZZLE_PASSWORD_FILE` | `""`    |
| `--no-analytics` | `DOZZLE_NO_ANALYTICS`  | false   |
| `--remote-host`  | `DOZZLE_REMOTE_HOST`   |         |

## Troubleshooting and FAQs

<details>
 <summary>I installed Dozzle, but logs are slow or they never load. Help!</summary>

Dozzle uses Server Sent Events (SSE) which connects to a server using a HTTP stream without closing the connection. If any proxy tries to buffer this connection, then Dozzle never receives the data and hangs forever waiting for the reverse proxy to flush the buffer. Since version `1.23.0`, Dozzle sends the `X-Accel-Buffering: no` header which should stop reverse proxies buffering. However, some proxies may ignore this header. In those cases, you need to explicitly disable any buffering.

Below is an example with nginx and using `proxy_pass` to disable buffering.

```
    server {
        ...

        location / {
            proxy_pass                  http://<dozzle.container.ip.address>:8080;
        }

        location /api {
            proxy_pass                  http://<dozzle.container.ip.address>:8080;

            proxy_buffering             off;
            proxy_cache                 off;
        }
    }

```

</details>

<details>
 <summary>What data does Dozzle collect?</summary>

Dozzle does collect some analytics. Analytics is anonymous usage tracking of the features which are used the most. See the section above on how to disable any analytic collection.

In the browser, Dozzle has a [strict](https://github.com/amir20/dozzle/blob/master/web/csp.go#L9) Content Security Policy which only allows the following policies:

- Allow connect to `api.github.com` to fetch most recent version.
- Only allow `<script>` and `<style>` files from `self`

Dozzle opens all links with `rel="noopener"`.

</details>

<details>
 <summary>We have tools that uses Dozzle when a new container is created. How can I get a direct link to a container by name?</summary>

Dozzle has a [special route](https://github.com/amir20/dozzle/blob/master/assets/pages/Show.vue) that can be used to search containers by name and then forward to that container. For example, if you have a container with name `"foo.bar"` and id `abc123`, you can send your users to `/show?name=foo.bar` which will be forwarded to `/container/abc123`.

</details>

<details>
 <summary>I installed Dozzle but memory consumption doesn't show up!</summary>

_This is an issue specific to ARM devices_

Dozzle uses the Docker API to gather information about the containers' memory usage. If the memory usage is not showing up, then it is likely that the Docker API is not returning the memory usage.

You can verify this by running `docker info`, and you should see the following:

```
WARNING: No memory limit support
WARNING: No swap limit support
```

In this case, you'll need to add the following line to your `/boot/cmdline.txt` file and reboot your device.

```
cgroup_enable=cpuset cgroup_enable=memory cgroup_memory=1
```

</details>

## License

[MIT](LICENSE)

## Building

To Build and test locally:

1. Install [NodeJs](https://nodejs.org/en/download/) and [pnpm](https://pnpm.io/installation).
2. Install [Go](https://go.dev/doc/install).
3. Install [reflex](https://github.com/cespare/reflex) with `go get -u github.com/cespare/reflex` outside of dozzle.
4. Install node modules `pnpm install`.
5. Do `pnpm dev`
