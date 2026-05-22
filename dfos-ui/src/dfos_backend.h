#pragma once

#include <QObject>
#include <QString>
#include <QVariantList>
#include <QTimer>

#include "logos_api.h"
#include "logos_api_client.h"

class DfosBackend : public QObject
{
    Q_OBJECT

    Q_PROPERTY(QString did      READ did      NOTIFY didChanged)
    Q_PROPERTY(QString status   READ status   NOTIFY statusChanged)
    Q_PROPERTY(QVariantList feed READ feed   NOTIFY feedChanged)

public:
    explicit DfosBackend(LogosAPI* logosAPI, QObject* parent = nullptr);

    QString did()           const { return m_did; }
    QString status()        const { return m_status; }
    QVariantList feed()     const { return m_feed; }

public slots:
    Q_INVOKABLE void createIdentity();
    Q_INVOKABLE void publishPost(const QString& text);
    Q_INVOKABLE void refreshFeed();

signals:
    void didChanged();
    void statusChanged();
    void feedChanged();
    void postError(const QString& message);

private:
    void init();
    QString callModule(const QString& method, const QString& arg = {});

    LogosAPI*         m_logosAPI;
    LogosAPIClient*   m_client;  // dfos_module client
    QString           m_did;
    QString           m_status;
    QVariantList      m_feed;
    QTimer            m_refreshTimer;
};
