// Minimal headless test: loads dfos_ui.so via QPluginLoader, verifies
// it exports IComponent, and creates the widget with a null LogosAPI.
// Run with: QT_QPA_PLATFORM=offscreen ./test_load <path_to_dfos_ui.so>

#include <QApplication>
#include <QPluginLoader>
#include <QObject>
#include <QWidget>
#include <QDebug>
#include <IComponent.h>

int main(int argc, char* argv[])
{
    QApplication app(argc, argv);

    if (argc < 2) {
        qCritical() << "Usage:" << argv[0] << "<path/to/dfos_ui.so>";
        return 1;
    }

    QString pluginPath = argv[1];
    QPluginLoader loader(pluginPath);

    if (!loader.load()) {
        qCritical() << "Failed to load plugin:" << loader.errorString();
        return 1;
    }
    qDebug() << "Plugin loaded OK:" << pluginPath;

    QObject* instance = loader.instance();
    if (!instance) {
        qCritical() << "loader.instance() returned nullptr";
        return 1;
    }

    IComponent* component = qobject_cast<IComponent*>(instance);
    if (!component) {
        qCritical() << "Plugin does not implement IComponent";
        return 1;
    }
    qDebug() << "IComponent interface OK";

    QWidget* widget = component->createWidget(nullptr);
    if (!widget) {
        qCritical() << "createWidget(nullptr) returned nullptr";
        return 1;
    }
    qDebug() << "createWidget(nullptr) OK, widget class:" << widget->metaObject()->className();

    component->destroyWidget(widget);
    loader.unload();

    qDebug() << "PASS: dfos_ui plugin loaded and validated";
    return 0;
}
