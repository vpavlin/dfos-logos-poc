#pragma once

#include <QObject>
#include <IComponent.h>

class DfosUiPlugin : public QObject, public IComponent
{
    Q_OBJECT
    Q_PLUGIN_METADATA(IID IComponent_iid FILE "metadata.json")
    Q_INTERFACES(IComponent)

public:
    explicit DfosUiPlugin(QObject* parent = nullptr);

    QWidget* createWidget(LogosAPI* logosAPI = nullptr) override;
    void destroyWidget(QWidget* widget) override;
};
