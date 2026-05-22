#include "dfos_ui_plugin.h"
#include "dfos_backend.h"

#include <QQuickWidget>
#include <QQmlContext>
#include <QDebug>
#include <QFile>
#include <QFileInfo>

DfosUiPlugin::DfosUiPlugin(QObject* parent)
    : QObject(parent)
{
    qDebug() << "DfosUiPlugin: created";
}

QWidget* DfosUiPlugin::createWidget(LogosAPI* logosAPI)
{
    qDebug() << "DfosUiPlugin::createWidget called";

    auto* quickWidget = new QQuickWidget();
    quickWidget->setResizeMode(QQuickWidget::SizeRootObjectToView);

    qmlRegisterType<DfosBackend>("DfosBackend", 1, 0, "DfosBackend");

    auto* backend = new DfosBackend(logosAPI, quickWidget);
    quickWidget->rootContext()->setContextProperty("backend", backend);

    QString qmlPath = "qrc:/DfosView.qml";
    QString envPath = qgetenv("DFOS_UI_QML_PATH");
    if (!envPath.isEmpty() && QFile::exists(envPath)) {
        qmlPath = QUrl::fromLocalFile(QFileInfo(envPath).absoluteFilePath()).toString();
        qDebug() << "DfosUiPlugin: loading QML from filesystem:" << qmlPath;
    }

    quickWidget->setSource(QUrl(qmlPath));
    if (quickWidget->status() == QQuickWidget::Error)
        qWarning() << "DfosUiPlugin: failed to load QML:" << quickWidget->errors();

    return quickWidget;
}

void DfosUiPlugin::destroyWidget(QWidget* widget)
{
    delete widget;
}
