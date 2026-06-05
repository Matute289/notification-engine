#!/bin/bash
# Seed MongoDB con templates de ejemplo
# Uso: ./deploy/scripts/seed-mongo.sh
# Correr una sola vez después del primer deploy

set -e

MONGO_USER="${MONGO_USER:-notif}"
MONGO_PASSWORD="${MONGO_PASSWORD:?Set MONGO_PASSWORD}"

echo "Seeding MongoDB templates..."

docker run --rm --network notification-engine_default \
  mongo:7 \
  mongosh \
  "mongodb://${MONGO_USER}:${MONGO_PASSWORD}@notif-mongodb:27017/notification_engine?authSource=admin" \
  --eval '
    db.notification_templates.insertMany([
      {
        _id: "11111111-1111-1111-1111-111111111111",
        name: "welcome",
        channel: "email",
        locale: "en",
        subject: "Welcome, {{.Name}}!",
        body: "Hi {{.Name}}, thanks for joining {{.Product}}.",
        media_urls: [],
        version: 1,
        owner_user_id: 1,
        created_at: new Date(),
        updated_at: new Date()
      },
      {
        _id: "22222222-2222-2222-2222-222222222222",
        name: "order_shipped",
        channel: "sms",
        locale: "en",
        subject: "",
        body: "Your order #{{.OrderID}} has shipped.",
        media_urls: [],
        version: 1,
        owner_user_id: 1,
        created_at: new Date(),
        updated_at: new Date()
      },
      {
        _id: "33333333-3333-3333-3333-333333333333",
        name: "game_request",
        channel: "push_ios",
        locale: "en",
        subject: "Game Request",
        body: "{{.From}} wants to play chess",
        media_urls: [],
        version: 1,
        owner_user_id: 1,
        created_at: new Date(),
        updated_at: new Date()
      }
    ], { ordered: false })
  ' && echo "✅ Seed completo" || echo "⚠️  Templates pueden ya existir (OK)"
