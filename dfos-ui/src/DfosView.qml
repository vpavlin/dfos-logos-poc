import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15

Rectangle {
    id: root
    color: "#1a1a2e"

    QtObject {
        id: theme
        readonly property color bg:        "#1a1a2e"
        readonly property color surface:   "#16213e"
        readonly property color surface2:  "#0f3460"
        readonly property color accent:    "#e94560"
        readonly property color accentDim: "#a33043"
        readonly property color text:      "#eaeaea"
        readonly property color textDim:   "#8888aa"
        readonly property color border:    "#2a2a4a"
        readonly property color success:   "#4ade80"
        readonly property color error:     "#ef4444"

        readonly property int fontNormal:  14
        readonly property int fontSmall:   12
        readonly property int fontLarge:   18
        readonly property int radius:       6
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 16
        spacing: 12

        // ── Header ──────────────────────────────────────────────────────
        Rectangle {
            Layout.fillWidth: true
            height: 52
            color: theme.surface
            radius: theme.radius
            border.color: theme.border
            border.width: 1

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: 14
                anchors.rightMargin: 14
                spacing: 12

                Text {
                    text: "DFOS"
                    font.pixelSize: theme.fontLarge
                    font.bold: true
                    color: theme.accent
                }

                Rectangle { width: 1; height: 28; color: theme.border }

                Text {
                    text: backend.did.length > 0
                          ? backend.did
                          : "No identity"
                    font.pixelSize: theme.fontSmall
                    color: backend.did.length > 0 ? theme.text : theme.textDim
                    elide: Text.ElideMiddle
                    Layout.fillWidth: true
                }

                Button {
                    text: backend.did.length > 0 ? "Identity created" : "Create Identity"
                    enabled: backend.did.length === 0 && backend.status === "ready"
                    implicitWidth: 140
                    implicitHeight: 32

                    onClicked: backend.createIdentity()

                    contentItem: Text {
                        text: parent.text
                        font.pixelSize: theme.fontSmall
                        color: parent.enabled ? theme.text : theme.textDim
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }

                    background: Rectangle {
                        color: parent.enabled
                               ? (parent.pressed ? theme.accentDim : theme.accent)
                               : theme.surface2
                        radius: theme.radius
                    }
                }

                Text {
                    text: backend.status
                    font.pixelSize: theme.fontSmall
                    color: backend.status === "ready"        ? theme.success
                         : backend.status === "initializing" ? theme.textDim
                         : theme.error
                }
            }
        }

        // ── Compose ─────────────────────────────────────────────────────
        Rectangle {
            Layout.fillWidth: true
            height: composeLayout.implicitHeight + 20
            color: theme.surface
            radius: theme.radius
            border.color: theme.border
            border.width: 1
            visible: backend.did.length > 0

            RowLayout {
                id: composeLayout
                anchors {
                    left: parent.left; right: parent.right
                    top: parent.top
                    margins: 10
                }
                spacing: 8

                TextField {
                    id: composeInput
                    Layout.fillWidth: true
                    placeholderText: "What's on your mind?"
                    font.pixelSize: theme.fontNormal
                    color: theme.text
                    wrapMode: TextInput.Wrap

                    background: Rectangle {
                        color: theme.bg
                        radius: theme.radius
                        border.color: composeInput.activeFocus ? theme.accent : theme.border
                        border.width: 1
                    }

                    Keys.onReturnPressed: if (event.modifiers & Qt.ControlModifier) publishBtn.clicked()
                }

                Button {
                    id: publishBtn
                    text: "Publish"
                    implicitWidth: 90
                    implicitHeight: 36
                    enabled: composeInput.text.trim().length > 0

                    onClicked: {
                        backend.publishPost(composeInput.text.trim())
                        composeInput.clear()
                    }

                    contentItem: Text {
                        text: parent.text
                        font.pixelSize: theme.fontSmall
                        color: parent.enabled ? theme.text : theme.textDim
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }

                    background: Rectangle {
                        color: parent.enabled
                               ? (parent.pressed ? theme.accentDim : theme.accent)
                               : theme.surface2
                        radius: theme.radius
                    }
                }
            }
        }

        // ── Feed header ─────────────────────────────────────────────────
        RowLayout {
            Layout.fillWidth: true

            Text {
                text: "Feed"
                font.pixelSize: theme.fontNormal
                font.bold: true
                color: theme.text
            }

            Text {
                text: backend.feed.length + " post" + (backend.feed.length !== 1 ? "s" : "")
                font.pixelSize: theme.fontSmall
                color: theme.textDim
            }

            Item { Layout.fillWidth: true }

            Button {
                text: "Refresh"
                implicitWidth: 80
                implicitHeight: 28

                onClicked: backend.refreshFeed()

                contentItem: Text {
                    text: parent.text
                    font.pixelSize: theme.fontSmall
                    color: theme.text
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                }

                background: Rectangle {
                    color: parent.pressed ? theme.surface2 : theme.surface
                    radius: theme.radius
                    border.color: theme.border
                    border.width: 1
                }
            }
        }

        // ── Feed list ────────────────────────────────────────────────────
        ScrollView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true

            background: Rectangle {
                color: theme.surface
                radius: theme.radius
                border.color: theme.border
                border.width: 1
            }

            ListView {
                id: feedList
                anchors.fill: parent
                anchors.margins: 8
                model: backend.feed
                spacing: 6

                delegate: Rectangle {
                    width: feedList.width
                    height: postCol.implicitHeight + 16
                    color: theme.bg
                    radius: theme.radius

                    Column {
                        id: postCol
                        anchors {
                            left: parent.left; right: parent.right
                            top: parent.top
                            margins: 10
                        }
                        spacing: 4

                        RowLayout {
                            width: parent.width

                            Text {
                                text: (modelData.creatorDID || "").substring(0, 32) + "…"
                                font.pixelSize: theme.fontSmall
                                font.bold: true
                                color: theme.accent
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }

                            Text {
                                text: modelData.createdAt ? modelData.createdAt.substring(0, 19).replace("T", " ") : ""
                                font.pixelSize: theme.fontSmall
                                color: theme.textDim
                            }
                        }

                        Text {
                            text: modelData.text || ""
                            font.pixelSize: theme.fontNormal
                            color: theme.text
                            wrapMode: Text.Wrap
                            width: parent.width
                        }
                    }
                }

                Text {
                    anchors.centerIn: parent
                    text: backend.did.length === 0
                          ? "Create an identity to start posting"
                          : "No posts yet — be the first!"
                    color: theme.textDim
                    font.pixelSize: theme.fontNormal
                    visible: feedList.count === 0 && backend.status === "ready"
                }
            }
        }

        // ── Error toast ──────────────────────────────────────────────────
        Rectangle {
            id: errorToast
            Layout.fillWidth: true
            height: 36
            color: theme.error
            radius: theme.radius
            visible: false
            opacity: 0

            Text {
                id: errorText
                anchors.centerIn: parent
                font.pixelSize: theme.fontSmall
                color: theme.text
            }

            Behavior on opacity { NumberAnimation { duration: 200 } }
        }
    }

    Connections {
        target: backend
        function onPostError(message) {
            errorText.text = message
            errorToast.visible = true
            errorToast.opacity = 1
            errorTimer.restart()
        }
    }

    Timer {
        id: errorTimer
        interval: 4000
        onTriggered: errorToast.opacity = 0
    }
}
