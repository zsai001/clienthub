import SwiftUI

struct ManagerView: View {
    @EnvironmentObject var appState: AppState

    var body: some View {
        VStack(spacing: 0) {
            managerConnectionBar
            Divider()

            if appState.serverManager.isConnected {
                connectedContent
            } else {
                VStack(spacing: 12) {
                    Spacer()
                    Image(systemName: "server.rack")
                        .font(.system(size: 48))
                        .foregroundStyle(.secondary)
                    Text("Not Connected")
                        .font(.title2.bold())
                    Text("Enter the admin address and secret, then click Connect.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                    Spacer()
                }
                .frame(maxWidth: .infinity)
            }
        }
    }

    // MARK: - Connection Bar

    private var managerConnectionBar: some View {
        HStack(spacing: 12) {
            Text("Admin")
                .font(.headline)

            TextField("host:port", text: $appState.serverManager.adminAddr)
                .textFieldStyle(.roundedBorder)
                .frame(width: 180)
                .disabled(appState.serverManager.isConnected)

            SecureField("secret", text: $appState.serverManager.secret)
                .textFieldStyle(.roundedBorder)
                .frame(width: 160)
                .disabled(appState.serverManager.isConnected)

            if appState.serverManager.isConnected {
                Button("Disconnect") {
                    appState.serverManager.disconnectManager()
                }
                .tint(.red)
            } else {
                Button("Connect") {
                    appState.serverManager.connectManager()
                    if appState.serverManager.isConnected {
                        appState.serverManager.refreshAll()
                    }
                }
                .disabled(appState.serverManager.adminAddr.isEmpty || appState.serverManager.secret.isEmpty)
            }

            Spacer()

            if appState.serverManager.isConnected {
                Button {
                    appState.serverManager.refreshAll()
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .help("Refresh all data")
            }

            if let err = appState.serverManager.errorMessage {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .lineLimit(1)
            }
        }
        .padding()
    }

    // MARK: - Connected Content

    private var connectedContent: some View {
        VSplitView {
            VStack(spacing: 0) {
                statusBar
                Divider()
                ClientListView()
                    .environmentObject(appState)
            }
            .frame(minHeight: 160)

            VStack(spacing: 0) {
                TunnelListView()
                    .environmentObject(appState)
                Divider()
                ForwardManagerView()
                    .environmentObject(appState)
            }
            .frame(minHeight: 160)
        }
    }

    private var statusBar: some View {
        HStack(spacing: 20) {
            if let status = appState.serverManager.serverStatus {
                Label("\(status.clientCount) Clients", systemImage: "person.2")
                Label("\(status.tunnelCount) Tunnels", systemImage: "arrow.left.arrow.right")
                Circle()
                    .fill(.green)
                    .frame(width: 8, height: 8)
                Text("Server Reachable")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
        }
        .padding(.horizontal)
        .padding(.vertical, 6)
        .background(Color(nsColor: .controlBackgroundColor))
    }
}
