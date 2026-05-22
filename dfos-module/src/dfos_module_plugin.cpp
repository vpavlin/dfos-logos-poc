#include "dfos_module_plugin.h"

#include <QDebug>
#include <QVariantList>
#include <QByteArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QThread>

#include "lib/dfos.h"

DfosModulePlugin* DfosModulePlugin::s_instance = nullptr;

static constexpr const char* kDeliveryModule  = "delivery_module";
static constexpr const char* kStorageModule   = "storage_module";
static constexpr const char* kWakuTopic       = "/dfos/1/operations/proto";
static constexpr const char* kStorageMapTopic = "/dfos/1/storage-map/proto";
static constexpr qint64      kChunkSize       = 65536;

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

    // ── Wire delivery_module messageReceived ──────────────────────────────────
    auto* delivClient = deliveryClient();
    if (delivClient) {
        LogosObject* replica = delivClient->requestObject(kDeliveryModule);
        if (replica) {
            delivClient->onEvent(replica, "messageReceived",
                [this](const QString& /*eventName*/, const QVariantList& data) {
                    if (data.size() < 3) return;
                    QString topic = data[1].toString();
                    QByteArray raw = QByteArray::fromBase64(data[2].toString().toUtf8());

                    if (topic == kWakuTopic) {
                        QString jws = QString::fromUtf8(raw);
                        qDebug() << "DfosModulePlugin: received op topic=" << topic
                                 << "jws_len=" << jws.size();
                        QByteArray jwsBytes = jws.toUtf8();
                        dfos_ingest_operation(jwsBytes.data());
                    } else if (topic == kStorageMapTopic) {
                        QJsonObject obj = QJsonDocument::fromJson(raw).object();
                        QString docCID     = obj.value("docCID").toString();
                        QString storageCID = obj.value("storageCID").toString();
                        QString creatorDID = obj.value("creatorDID").toString();
                        qDebug() << "DfosModulePlugin: received storage-map docCID=" << docCID
                                 << "storageCID=" << storageCID;
                        if (!docCID.isEmpty() && !storageCID.isEmpty()) {
                            QByteArray d = docCID.toUtf8(), s = storageCID.toUtf8(), c = creatorDID.toUtf8();
                            dfos_set_storage_cid(d.data(), s.data(), c.data());
                            downloadBlobFromStorage(storageCID, creatorDID, docCID);
                        }
                    }
                });
            qDebug() << "DfosModulePlugin: wired messageReceived";
        } else {
            qWarning() << "DfosModulePlugin: could not request delivery_module replica";
        }
        delivClient->invokeRemoteMethod(kDeliveryModule, "subscribe", QString(kWakuTopic));
        delivClient->invokeRemoteMethod(kDeliveryModule, "subscribe", QString(kStorageMapTopic));
        qDebug() << "DfosModulePlugin: subscribed to" << kWakuTopic << "and" << kStorageMapTopic;
    } else {
        qWarning() << "DfosModulePlugin: delivery_module client unavailable";
    }

    // ── Wire storage_module events ────────────────────────────────────────────
    auto* storClient = storageClient();
    if (storClient) {
        LogosObject* storReplica = storClient->requestObject(kStorageModule);
        if (storReplica) {
            // Collect download chunks.
            storClient->onEvent(storReplica, "storageDownloadProgress",
                [this](const QString&, const QVariantList& data) {
                    if (data.isEmpty()) return;
                    QJsonObject obj = QJsonDocument::fromJson(
                        data[0].toString().toUtf8()).object();
                    QString sid     = obj.value("sessionId").toString();
                    QString chunk   = obj.value("chunk").toString();
                    if (sid.isEmpty() || chunk.isEmpty()) return;

                    QMutexLocker lk(&m_pendingMu);
                    if (!m_downloadedData.contains(sid))
                        m_downloadedData[sid] = QByteArray();
                    m_downloadedData[sid].append(chunk.toUtf8());
                });

            // Finalise download: persist blob + release waiting thread.
            storClient->onEvent(storReplica, "storageDownloadDone",
                [this](const QString&, const QVariantList& data) {
                    if (data.isEmpty()) return;
                    QJsonObject obj = QJsonDocument::fromJson(
                        data[0].toString().toUtf8()).object();
                    QString sid     = obj.value("sessionId").toString();
                    bool    success = obj.value("success").toBool();

                    QMutexLocker lk(&m_pendingMu);
                    QVariantMap meta = m_pendingDownloads.value(sid).toMap();
                    QString creatorDID = meta.value("creatorDID").toString();
                    QString docCID     = meta.value("docCID").toString();

                    if (success && !creatorDID.isEmpty() && !docCID.isEmpty()) {
                        QByteArray blob = m_downloadedData.value(sid);
                        if (!blob.isEmpty()) {
                            QByteArray cdid   = creatorDID.toUtf8();
                            QByteArray ddocid = docCID.toUtf8();
                            dfos_put_blob_for_content(cdid.data(), ddocid.data(), blob.data());
                            qDebug() << "DfosModulePlugin: stored downloaded blob"
                                     << docCID << blob.size() << "bytes";
                        }
                    }

                    // Release any thread waiting on this download.
                    if (m_downloadSems.contains(sid))
                        m_downloadSems[sid]->release();

                    m_pendingDownloads.remove(sid);
                    m_downloadedData.remove(sid);
                });

            qDebug() << "DfosModulePlugin: wired storage_module events";
        } else {
            qWarning() << "DfosModulePlugin: could not request storage_module replica";
        }
    } else {
        qWarning() << "DfosModulePlugin: storage_module client unavailable — blobs stored locally";
    }

    // ── Initialise Go relay ───────────────────────────────────────────────────
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

    // Upload blob to storage_module and broadcast the docCID→storageCID mapping
    // so peer nodes can retrieve the blob. Done inline (not async) so the
    // mapping is published before the caller continues.
    QJsonObject resObj = QJsonDocument::fromJson(s.toUtf8()).object();
    QString contentId  = resObj.value("contentId").toString();
    if (!contentId.isEmpty() && storageClient())
        asyncStoreBlob(contentId);

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

// ── Storage helpers ───────────────────────────────────────────────────────────

void DfosModulePlugin::asyncStoreBlob(const QString& contentId)
{
    // dfos_get_blob_for_content is thread-safe (reads from SQLite).
    QByteArray cid = contentId.toUtf8();
    char* raw = dfos_get_blob_for_content(cid.data());
    if (!raw) return;
    QJsonObject meta = QJsonDocument::fromJson(QByteArray(raw)).object();
    dfos_free(raw);

    QString docCID     = meta.value("docCID").toString();
    QString creatorDID = meta.value("creatorDID").toString();
    QString blobData   = meta.value("data").toString();

    if (docCID.isEmpty() || blobData.isEmpty()) return;

    QString storageCID = uploadBlobToStorage(docCID, creatorDID, blobData);
    if (storageCID.isEmpty()) {
        qWarning() << "DfosModulePlugin: storage upload failed for" << docCID;
        return;
    }
    qDebug() << "DfosModulePlugin: blob stored docCID=" << docCID
             << "storageCID=" << storageCID;

    // Persist the mapping locally.
    {
        QByteArray d = docCID.toUtf8(), s = storageCID.toUtf8(), c = creatorDID.toUtf8();
        dfos_set_storage_cid(d.data(), s.data(), c.data());
    }

    // Broadcast the docCID → storageCID mapping so peer nodes can fetch the blob.
    QJsonObject mapMsg;
    mapMsg["docCID"]     = docCID;
    mapMsg["storageCID"] = storageCID;
    mapMsg["creatorDID"] = creatorDID;
    QString mapPayload = QJsonDocument(mapMsg).toJson(QJsonDocument::Compact);
    if (auto* dc = deliveryClient())
        dc->invokeRemoteMethod(kDeliveryModule, "send",
            QString(kStorageMapTopic), mapPayload);
    qDebug() << "DfosModulePlugin: published storage-map for docCID=" << docCID;
}

QString DfosModulePlugin::uploadBlobToStorage(const QString& docCID,
                                               const QString& /*creatorDID*/,
                                               const QString& blobData)
{
    auto* sc = storageClient();
    if (!sc) return {};

    // storage_module returns StdLogosResult serialized as JSON: {"success":bool,"value":...,"error":...}
    auto parseResult = [](const QVariant& v) -> QJsonObject {
        return QJsonDocument::fromJson(v.toString().toUtf8()).object();
    };

    // uploadInit — synchronous, returns JSON {success, value: sessionId}.
    QJsonObject o1 = parseResult(sc->invokeRemoteMethod(kStorageModule, "uploadInit",
        QVariant(docCID), QVariant(kChunkSize)));
    if (!o1.value("success").toBool()) {
        qWarning() << "DfosModulePlugin: uploadInit failed:" << o1.value("error").toString();
        return {};
    }
    QString sessionId = o1.value("value").toString();

    // uploadChunk — queues chunk; storage_module processes asynchronously.
    QJsonObject o2 = parseResult(sc->invokeRemoteMethod(kStorageModule, "uploadChunk",
        QVariant(sessionId), QVariant(blobData)));
    if (!o2.value("success").toBool()) {
        qWarning() << "DfosModulePlugin: uploadChunk failed:" << o2.value("error").toString();
        return {};
    }

    // uploadFinalize — synchronous, returns JSON {success, value: CID}.
    QJsonObject o3 = parseResult(sc->invokeRemoteMethod(kStorageModule, "uploadFinalize",
        QVariant(sessionId)));
    if (!o3.value("success").toBool()) {
        qWarning() << "DfosModulePlugin: uploadFinalize failed:" << o3.value("error").toString();
        return {};
    }
    return o3.value("value").toString();
}

bool DfosModulePlugin::downloadBlobFromStorage(const QString& storageCID,
                                                const QString& creatorDID,
                                                const QString& docCID)
{
    auto* sc = storageClient();
    if (!sc) return false;

    // downloadChunks — returns JSON {success, value: sessionId}; events are async.
    QJsonObject rd = QJsonDocument::fromJson(
        sc->invokeRemoteMethod(kStorageModule, "downloadChunks",
            QVariant(storageCID), QVariant(false), QVariant(kChunkSize)).toString().toUtf8()).object();
    if (!rd.value("success").toBool()) {
        qWarning() << "DfosModulePlugin: downloadChunks failed:" << rd.value("error").toString();
        return false;
    }
    QString sessionId = rd.value("value").toString();

    // Register metadata keyed by sessionId so the storageDownloadDone handler can persist.
    QMutexLocker lk(&m_pendingMu);
    QVariantMap meta;
    meta["creatorDID"] = creatorDID;
    meta["docCID"]     = docCID;
    m_pendingDownloads[sessionId] = meta;

    qDebug() << "DfosModulePlugin: download started storageCID=" << storageCID
             << "sessionId=" << sessionId;
    return true;
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

LogosAPIClient* DfosModulePlugin::storageClient()
{
    if (!logosAPI) return nullptr;
    return logosAPI->getClient(kStorageModule);
}
