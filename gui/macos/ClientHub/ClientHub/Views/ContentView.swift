import SwiftUI

struct ContentView: View {
    @EnvironmentObject var appState: AppState

    var body: some View {
        TabView {
            ClientView()
                .tabItem {
                    Label("Client", systemImage: "network")
                }

            ManagerView()
                .tabItem {
                    Label("Manager", systemImage: "server.rack")
                }
        }
        .padding()
    }
}
