# Rendezvous Key-Value Server

A lightweight, public key-value store server designed as a rendezvous/bootstrap point for mesh networks, distributed applications, and general-purpose key-value storage without registration.

## 🌐 Publicly Hosted Instance

A public instance is available at: [https://rendezvous.jipok.ru](https://rendezvous.jipok.ru)

Key limit: 100 bytes

Value limit: 1000 bytes

Rate limit: 11 tokens per minute (POST=3 tokens, GET=1 token)

Expire time: 2 hours

## 🚀 API Usage

### Store a Value

```bash
curl -X POST -d "your-data-here" https://rendezvous.jipok.ru/your-key
```

The expiration time for a key is reset with every successful POST request, extending its lifetime.

### Retrieve a Value

```bash
curl https://rendezvous.jipok.ru/your-key
```

### Protecting Values with Owner Secret

You can protect your values from modification by adding the `X-Owner-Secret` header when posting:

```bash
# Store a value with owner protection
curl -X POST -d "your-protected-data" -H "X-Owner-Secret: your-secret-here" https://rendezvous.jipok.ru/your-key

# Update a protected value (requires the same secret)
curl -X POST -d "your-new-data" -H "X-Owner-Secret: your-secret-here" https://rendezvous.jipok.ru/your-key

# This will be rejected if the secret doesn't match
curl -X POST -d "unauthorized-update" -H "X-Owner-Secret: wrong-secret" https://rendezvous.jipok.ru/your-key
```

Anyone can still read the value, but only someone with the correct secret can modify it.

**Note**: The secret and the value together must not exceed the maximum value size limit (1000 bytes by default).

### IP-Protected Keys

For paths prefixed with `/ip/`, the server automatically injects the client's IP address into the key:

```bash
# If your client IP is 20.18.12.10, this actually stores the value under
# the key "ip/20.18.12.10/service1" automatically.
# The response will return your public IP address instead of "OK"
curl -X POST -d "my-server-info" https://rendezvous.jipok.ru/ip/service1

curl https://rendezvous.jipok.ru/ip/20.18.12.10/service1
```

This feature makes it easy for servers to publish information that only they can modify, without needing to know their public IP in advance. Stored key is automatically prefixed with client's IP, preventing others from overwriting the data.

## 📋 Use Cases

- **Peer Discovery**: Help distributed systems and mesh networks discover initial peers
- **Configuration Distribution**: Share configuration files or bootstrap information
- **Temporary Data Exchange**: Easy way to share ephemeral data between systems
- **IoT Device Coordination**: Simple communication point for IoT devices
- **Secure Self-Identification**: Servers can publish their details under IP-protected keys

# 🔧 Self-hosted

### Using prebuilt

```bash
wget https://github.com/Jipok/rendezvous/releases/latest/download/rendezvous-server
chmod +x rendezvous-server
./rendezvous-server
```

### Using Go

```bash
git clone https://github.com/Jipok/rendezvous
cd rendezvous
go build
./rendezvous-server
```

## 🔑 Features

- **No Registration**: Just use it directly, no accounts needed
- **Simple HTTP API**: Store and retrieve values with basic HTTP GET/POST requests
- **Ephemeral Storage**: Keys automatically expire after configured time
- **Rate Limited**: Basic protection against abuse (token-based rate limiting per IP)
- **Configurable Limits**: Adjustable key/value sizes and storage capacity
- **Zero Dependencies**: Just the server, no databases needed
- **Value Protection**: Optional secret-based protection for value updates


## ⚙️ Configuration Options

| Flag                   | Default        | Description                                                 |
|------------------------|----------------|-------------------------------------------------------------|
| -maxKeySize            | 100            | Maximum key length in bytes                                 |
| -maxValueSize          | 1000           | Maximum value size in bytes (including secret)              |
| -maxNumKV              | 100000         | Maximum number of key-value pairs                           |
| -expireDuration        | 2h             | Time after which keys expire                                |
| -resetDuration         | 1m             | Duration between rate limit resets                          |
| -saveDuration          | 30m            | Duration between state saves                                |
| -maxRequests           | 11             | Maximum request tokens per IP (POST=3 tokens, GET=1 token)  |
| -port                  | 80             | Server port                                                 |
| -l                     | 0.0.0.0        | Interface to listen on                                      |
| -disableLocalIPWaring  | false          | Disable warnings about requests from localhost              |

Example:

```bash
./rendezvous-server -maxValueSize 4096 -expireDuration 24h -port 9000
```

## ⚠️ Limitations

- **Ephemeral Storage**: All data is temporary and will be deleted after expiration
- **No Encryption**: Data is stored and transmitted without encryption(except TLS)
- **Size Limits**: Value size limit includes owner secret if used
- **IPv4 Only**: For rate-limit purpose supports only IPv4 addresses