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

For keys prefixed with `/ip/`, server enforces that part after `/ip/` begins with client's IP address:

```bash
# This will work if your IP is 20.18.12.10
curl -X POST -d "my-server-info" https://rendezvous.jipok.ru/ip/20.18.12.10/service1

# This will be rejected if your IP is not 101.50.0.191
curl -X POST -d "spoofed-data" https://rendezvous.jipok.ru/ip/101.50.0.191/service1
```

This feature ensures servers can publish information about themselves that others cannot overwrite.

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
- **Rate Limited**: Basic protection against abuse (one POST per IP per minute)
- **Configurable Limits**: Adjustable key/value sizes and storage capacity
- **Zero Dependencies**: Just the server, no databases needed
- **Value Protection**: Optional secret-based protection for value updates


## ⚙️ Configuration Options

| Flag            | Default        | Description                                    |
|-----------------|----------------|------------------------------------------------|
| -maxKeySize     | 100            | Maximum key length in bytes                    |
| -maxValueSize   | 1000           | Maximum value size in bytes (including secret) |
| -maxNumKV       | 100000         | Maximum number of key-value pairs              |
| -expireDuration | 2h             | Time after which keys expire                   |
| -resetDuration  | 1m             | Duration between rate limit resets             |
| -saveDuration   | 30m            | Duration between state saves                   |
| -port           | 80             | Server port                                    |
| -l              | 0.0.0.0        | Interface to listen on                         |

Example:

```bash
./rendezvous-server -maxValueSize 4096 -expireDuration 24h -port 9000
```

## ⚠️ Limitations

- **Ephemeral Storage**: All data is temporary and will be deleted after expiration
- **No Encryption**: Data is stored and transmitted without encryption(except TLS)
- **Size Limits**: Value size limit includes owner secret if used
- **Rate Limiting**: Only one POST request per IP address per minute
- **IPv4 Only**: For rate-limit purpose supports only IPv4 addresses