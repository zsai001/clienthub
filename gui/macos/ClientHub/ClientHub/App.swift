import SwiftUI

@main
struct ClientHubApp: App {
    @StateObject private var appState = AppState()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(appState)
                .frame(minWidth: 800, minHeight: 560)
        }
        .windowStyle(.titleBar)
        .defaultSize(width: 960, height: 640)

        Settings {
            SettingsView()
                .environmentObject(appState)
        }
    }
}
