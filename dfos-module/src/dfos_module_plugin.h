#pragma once

#include <QObject>
#include <QString>
#include <QMutex>

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

    // Called after delivery_module is started; dataDir is where SQLite + keys are stored.
    Q_INVOKABLE QString start(const QString& dataDir);

    Q_INVOKABLE QString createIdentity();
    Q_INVOKABLE QString getIdentity();
    Q_INVOKABLE QString publishPost(const QString& text);
    Q_INVOKABLE QString getFeed(int limit);

signals:
    void eventResponse(const QString& eventName, const QVariantList& args);

private:
    LogosAPIClient* deliveryClient();

    // Static C callbacks passed to the Go shared library.
    static void wakuPublishCb(const char* topic, const char* payload);

    static DfosModulePlugin* s_instance;
};
