#pragma once

#include <QObject>
#include <QString>
#include <QMutex>
#include <QSemaphore>
#include <QVariantMap>

#include "interface.h"
#include "logos_api.h"
#include "logos_api_client.h"
#include "logos_object.h"

class DfosModulePlugin : public QObject, public PluginInterface
{
    Q_OBJECT
    Q_PLUGIN_METADATA(IID "dfos_module_plugin" FILE "metadata.json")
    Q_INTERFACES(PluginInterface)

public:
    explicit DfosModulePlugin(QObject* parent = nullptr);
    ~DfosModulePlugin() override;

    QString name() const override { return "dfos_module"; }
    QString version() const override { return "0.1.0"; }

    Q_INVOKABLE void initLogos(LogosAPI* api);

    // Called after delivery_module is started; dataDir is where SQLite + keys live.
    Q_INVOKABLE QString start(const QString& dataDir);

    Q_INVOKABLE QString createIdentity();
    Q_INVOKABLE QString getIdentity();
    Q_INVOKABLE QString publishPost(const QString& text);
    Q_INVOKABLE QString getFeed(int limit);

signals:
    void eventResponse(const QString& eventName, const QVariantList& args);

private:
    // ── delivery_module helpers ───────────────────────────────────────────────
    LogosAPIClient* deliveryClient();

    // ── storage_module helpers ────────────────────────────────────────────────
    LogosAPIClient* storageClient();

    // Upload blob to storage_module (synchronous: init→chunk→finalize).
    // Returns the storage CID, or empty string on failure.
    QString uploadBlobToStorage(const QString& docCID,
                                const QString& creatorDID,
                                const QString& blobData);

    // Download blob from storage_module and persist locally (async via events).
    // storageCID: the CID returned by a previous upload.
    // Returns false if the download request could not be sent.
    bool downloadBlobFromStorage(const QString& storageCID,
                                 const QString& creatorDID,
                                 const QString& docCID);

    // Async upload worker: called from a QThread so the Qt main thread isn't
    // blocked while storage_module processes the upload.
    void asyncStoreBlob(const QString& contentId);

    // ── Static C callbacks ────────────────────────────────────────────────────
    static void wakuPublishCb(const char* topic, const char* payload);

    // ── State ─────────────────────────────────────────────────────────────────
    static DfosModulePlugin* s_instance;

    // logosAPI is inherited from PluginInterface (checked by the framework in callMethod).

    // Pending download: storageCID → {creatorDID, docCID} for the download done handler.
    QMutex          m_pendingMu;
    QVariantMap     m_pendingDownloads; // storageCID → QVariantMap{creatorDID, docCID}

    // Semaphore released by storageDownloadDone for a given storageCID.
    QMap<QString, QSemaphore*> m_downloadSems;
    QMap<QString, QByteArray>  m_downloadedData; // storageCID → collected chunks
};
