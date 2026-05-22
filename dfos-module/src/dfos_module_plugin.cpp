#include "dfos_module_plugin.h"

#include <QDebug>
#include <QVariantList>
#include <QByteArray>

#include "lib/dfos.h"

DfosModulePlugin* DfosModulePlugin::s_instance = nullptr;

static constexpr const char* kDeliveryModule = "delivery_module";
static constexpr const char* kWakuTopic      = "/dfos/1/operations/proto";

DfosModulePlugin::DfosModulePlugin(QObject* parent)
    : QObject(parent)
{
    s_instance = this;
    qDebug() << "DfosModulePlugin: created";
}

DfosModulePlugin::~DfosModulePlugin()
{
    if (s_instance == this)
        s_instance = nullptr;
}

void DfosModulePlugin::initLogos(LogosAPI* api)
{
    logosAPI = api;
    qDebug() << "DfosModulePlugin: LogosAPI initialized";
}

QString DfosModulePlugin::start(const QString& dataDir)
{
    if (!logosAPI) {
        qWarning() << "DfosModulePlugin::start: LogosAPI not set";
        return R"({"error":"LogosAPI not initialized"})";
    }

    // Wire the messageReceived event from delivery_module.
    auto* client = deliveryClient();
    if (client) {
        LogosObject* replica = client->requestObject(kDeliveryModule);
        if (replica) {
            client->onEvent(replica, "messageReceived",
                [this](const QString& /*eventName*/, const QVariantList& data) {
                    if (data.size() < 3) return;
                    // data[2] = payload (base64 of the raw JWS token string)
                    QByteArray raw = QByteArray::fromBase64(data[2].toString().toUtf8());
                    QString jws = QString::fromUtf8(raw);
                    qDebug() << "DfosModulePlugin: received op topic=" << data[1].toString()
                             << "jws_len=" << jws.size();
                    QByteArray jwsBytes = jws.toUtf8();
                    dfos_ingest_operation(jwsBytes.data());
                });
            qDebug() << "DfosModulePlugin: wired messageReceived";
        } else {
            qWarning() << "DfosModulePlugin: could not request delivery_module replica";
        }

        // Subscribe to the DFOS proof plane topic.
        client->invokeRemoteMethod(kDeliveryModule, "subscribe", QString(kWakuTopic));
        qDebug() << "DfosModulePlugin: subscribed to" << kWakuTopic;
    } else {
        qWarning() << "DfosModulePlugin: delivery_module client unavailable — Waku gossip disabled";
    }

    // Initialise the Go shared library.
    QByteArray dataDirBytes = dataDir.toUtf8();
    char* errPtr = dfos_init(dataDirBytes.data(), wakuPublishCb, nullptr, nullptr);
    if (errPtr) {
        QString err = QString::fromUtf8(errPtr);
        dfos_free(errPtr);
        qWarning() << "DfosModulePlugin: dfos_init failed:" << err;
        return err;
    }

    qDebug() << "DfosModulePlugin: started, dataDir=" << dataDir;
    return "{}";
}

QString DfosModulePlugin::createIdentity()
{
    char* result = dfos_create_identity();
    if (!result) return "{}";
    QString s = QString::fromUtf8(result);
    dfos_free(result);
    return s;
}

QString DfosModulePlugin::getIdentity()
{
    char* result = dfos_get_identity();
    if (!result) return R"({"did":null})";
    QString s = QString::fromUtf8(result);
    dfos_free(result);
    return s;
}

QString DfosModulePlugin::publishPost(const QString& text)
{
    QByteArray textBytes = text.toUtf8();
    char* result = dfos_publish_post(textBytes.data());
    if (!result) return "{}";
    QString s = QString::fromUtf8(result);
    dfos_free(result);
    return s;
}

QString DfosModulePlugin::getFeed(int limit)
{
    char* result = dfos_get_feed(limit);
    if (!result) return "[]";
    QString s = QString::fromUtf8(result);
    dfos_free(result);
    return s;
}

// ── Static callbacks ─────────────────────────────────────────────────────────

void DfosModulePlugin::wakuPublishCb(const char* topic, const char* payload)
{
    if (!s_instance || !s_instance->logosAPI) return;

    auto* client = s_instance->logosAPI->getClient(kDeliveryModule);
    if (!client) return;

    client->invokeRemoteMethod(kDeliveryModule, "send",
        QString::fromUtf8(topic),
        QString::fromUtf8(payload));
}

// ── Helpers ──────────────────────────────────────────────────────────────────

LogosAPIClient* DfosModulePlugin::deliveryClient()
{
    if (!logosAPI) return nullptr;
    return logosAPI->getClient(kDeliveryModule);
}
