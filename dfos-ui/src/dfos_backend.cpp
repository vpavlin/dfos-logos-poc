#include "dfos_backend.h"

#include <QDebug>
#include <QJsonDocument>
#include <QJsonObject>
#include <QJsonArray>

static constexpr const char* kDfosModule = "dfos_module";
static constexpr const char* kDataDir = "/tmp/dfos-ui-data";

DfosBackend::DfosBackend(LogosAPI* logosAPI, QObject* parent)
    : QObject(parent)
    , m_logosAPI(logosAPI)
    , m_client(nullptr)
    , m_status("initializing")
{
    if (m_logosAPI)
        m_client = m_logosAPI->getClient(kDfosModule);

    QTimer::singleShot(0, this, [this] { init(); });

    m_refreshTimer.setInterval(5000);
    connect(&m_refreshTimer, &QTimer::timeout, this, &DfosBackend::refreshFeed);
}

void DfosBackend::init()
{
    if (!m_client) {
        m_status = "error: no dfos_module client";
        emit statusChanged();
        return;
    }

    QString result = callModule("start", kDataDir);
    if (result.isEmpty() || result == "{}") {
        // Load existing identity if any
        QString idResult = callModule("getIdentity");
        QJsonDocument doc = QJsonDocument::fromJson(idResult.toUtf8());
        if (doc.isObject()) {
            QString did = doc.object().value("did").toString();
            if (!did.isNull() && !did.isEmpty()) {
                m_did = did;
                emit didChanged();
            }
        }
        m_status = "ready";
        emit statusChanged();
        refreshFeed();
        m_refreshTimer.start();
    } else {
        m_status = "error: " + result;
        emit statusChanged();
    }
}

QString DfosBackend::callModule(const QString& method, const QString& arg)
{
    if (!m_client) return {};

    QVariant result;
    if (arg.isEmpty())
        result = m_client->invokeRemoteMethod(kDfosModule, method);
    else
        result = m_client->invokeRemoteMethod(kDfosModule, method, QVariant(arg));

    return result.toString();
}

void DfosBackend::createIdentity()
{
    if (!m_client) return;

    QString result = callModule("createIdentity");
    QJsonDocument doc = QJsonDocument::fromJson(result.toUtf8());
    if (doc.isObject()) {
        QString did = doc.object().value("did").toString();
        if (!did.isNull() && !did.isEmpty()) {
            m_did = did;
            emit didChanged();
        } else if (doc.object().contains("error")) {
            qWarning() << "DfosBackend: createIdentity error:" << doc.object().value("error");
        }
    }
}

void DfosBackend::publishPost(const QString& text)
{
    if (!m_client || m_did.isEmpty()) {
        emit postError("No identity — create one first");
        return;
    }
    if (text.trimmed().isEmpty()) return;

    m_client->invokeRemoteMethodAsync(kDfosModule, "publishPost", QVariant(text),
        [this](QVariant result) {
            QString r = result.toString();
            QJsonDocument doc = QJsonDocument::fromJson(r.toUtf8());
            if (doc.isObject() && doc.object().contains("error")) {
                qWarning() << "DfosBackend: publishPost error:" << r;
                emit postError(doc.object().value("error").toString());
            } else {
                refreshFeed();
            }
        });
}

void DfosBackend::refreshFeed()
{
    if (!m_client) return;

    m_client->invokeRemoteMethodAsync(kDfosModule, "getFeed", QVariant(50),
        [this](QVariant result) {
            QString r = result.toString();
            QJsonDocument doc = QJsonDocument::fromJson(r.toUtf8());
            if (!doc.isArray()) return;

            QVariantList newFeed;
            for (const QJsonValue& v : doc.array()) {
                if (!v.isObject()) continue;
                newFeed.append(v.toObject().toVariantMap());
            }
            m_feed = newFeed;
            emit feedChanged();
        });
}
