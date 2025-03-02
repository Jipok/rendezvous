# Rendezvous Key-Value Server

A lightweight, public key-value store server designed as a rendezvous/bootstrap point for mesh networks, distributed applications, and general-purpose key-value storage without registration.

## 🌐 Publicly Hosted Instance

A public instance is available at: [https://rendezvous.jipok.ru](https://rendezvous.jipok.ru)

Key limit: 100 bytes

Value limit: 1000 bytes

Rate limit: 1 POST per minute

Expire time: 2 hour

## 🚀 API Usage

### Store a Value

```bash
curl -X POST -d "your-data-here" https://rendezvous.jipok.ru/your-key
```

### Retrieve a Value

```bash
curl https://rendezvous.jipok.ru/your-key
```

## 📋 Use Cases

- **Peer Discovery**: Help distributed systems and mesh networks discover initial peers
- **Configuration Distribution**: Share configuration files or bootstrap information
- **Temporary Data Exchange**: Easy way to share ephemeral data between systems
- **IoT Device Coordination**: Simple communication point for IoT devices

# 🔧 Self-hosted

### Using prebuilt

```bash
wget https://github.com/Jipok/rendezvous/releases/latest/download/rendezvous-server
chmod +x rendezvous-server
./rendezvous-server
```

### Using Go

```bash
git clone https://github.com/Jipok/rendezvous-kv
cd rendezvous-kv
go build
./rendezvous-kv
```

## 🔑 Features

- **No Registration**: Just use it directly, no accounts needed
- **Simple HTTP API**: Store and retrieve values with basic HTTP GET/POST requests
- **Ephemeral Storage**: Keys automatically expire after configured time
- **Rate Limited**: Basic protection against abuse (one POST per IP per minute)
- **Configurable Limits**: Adjustable key/value sizes and storage capacity
- **Zero Dependencies**: Just the server, no databases needed


## ⚙️ Configuration Options

| Flag            | Default        | Description                                  |
|-----------------|----------------|----------------------------------------------|
| -maxKeySize     | 100            | Maximum key length in bytes                  |
| -maxValueSize   | 1000           | Maximum value size in bytes                  |
| -maxNumKV       | 500000         | Maximum number of key-value pairs            |
| -expireDuration | 1h             | Time after which keys expire                 |
| -resetDuration  | 1m             | Duration between rate limit resets           |
| -saveDuration   | 30m            | Duration between state saves                 |
| -port           | 8080           | Server port                                  |
| -l              | 0.0.0.0        | Interface to listen on                       |

Example:

```bash
./rendezvous-server -maxValueSize 4096 -expireDuration 24h -port 9000
```

## ⚠️ Limitations

- **Ephemeral Storage**: All data is temporary and will be deleted after expiration
- **No Encryption**: Data is stored and transmitted(except TLS) without encryption
- **No Authentication**: Anyone can read any key
- **Rate Limiting**: Only one POST request per IP address per minute
- **IPv4 Only**: For rate-limit purpose supports only IPv4 addresses
