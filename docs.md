# API Documentation Links

## Swagger UI

- Identity Service: [http://localhost/identity/swagger/index.html](http://localhost/identity/swagger/index.html)
- Notification Service: [http://localhost/notification/swagger/index.html](http://localhost/notification/swagger/index.html)

## Monitoring & Logging

- Grafana Dashboard: [http://localhost/grafana](http://localhost/grafana)
  - Account: `admin`
  - Password: `admin123`
- Jaeger UI (Distributed Tracing): [http://jaeger.localhost](http://jaeger.localhost)

## Database, Cache & Message Broker GUI Tools

> Yêu cầu thêm vào hosts file một lần (`C:\Windows\System32\drivers\etc\hosts`):
> ```
> 127.0.0.1 adminer.localhost
> 127.0.0.1 redisinsight.localhost
> ```

- **CloudBeaver** (PostgreSQL): [http://cloudbeaver.localhost](http://cloudbeaver.localhost) — web DBeaver
  - Account: `admin`
  - Password: `password123`

  | | Identity DB | Notification DB |
  |---|---|---|
  | Host | `postgres-identity-service` | `postgres-notification-service` |
  | Port | `5432` | `5432` |
  | Username | `user_admin` | `user_admin` |
  | Password | `password123` | `password123` |
  | Database | `identity_db` | `notification_db` |
  | Admin Username | `user_admin` | `user_admin` |
  | Admin Password | `Toai20102002` | `Toai20102002` |

- **RedisInsight** (Redis): [http://redisinsight.localhost](http://redisinsight.localhost)
  - Identity Redis: host `redis-identity-service`, port `6379`
  - Notification Redis: host `redis-notification-service`, port `6379`

- **RabbitMQ Management UI**: [http://rabbitmq.localhost](http://rabbitmq.localhost)
  - Username: `guest`
  - Password: `guest`

> Thêm vào hosts file một lần (`C:\Windows\System32\drivers\etc\hosts`):
> ```
> 127.0.0.1 cloudbeaver.localhost
> 127.0.0.1 redisinsight.localhost
> 127.0.0.1 jaeger.localhost
> 127.0.0.1 rabbitmq.localhost
> ```

