import SwiftUI

struct TunnelListView: View {
    @EnvironmentObject var appState: AppState

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Active Tunnels")
                    .font(.subheadline.bold())
                Spacer()
                Button {
                    appState.serverManager.fetchTunnels()
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .buttonStyle(.borderless)
                .controlSize(.small)
            }
            .padding(.horizontal)
            .padding(.vertical, 6)

            if appState.serverManager.tunnels.isEmpty {
                Text("No active tunnels")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding()
            } else {
                Table(appState.serverManager.tunnels) {
                    TableColumn("Session") { t in
                        Text("\(t.sessionID)")
                    }
                    .width(60)
                    TableColumn("Source", value: \.sourceClient)
                    TableColumn("Target", value: \.targetClient)
                    TableColumn("Service", value: \.targetService)
                    TableColumn("Protocol", value: \.protocol)
                        .width(60)
                }
            }
        }
    }
}
