import SwiftUI

struct ClientListView: View {
    @EnvironmentObject var appState: AppState

    @State private var selection: ClientInfoItem.ID?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Connected Clients")
                    .font(.subheadline.bold())
                Spacer()

                if let sel = selection,
                   let client = appState.serverManager.clients.first(where: { $0.id == sel }) {
                    Button("Kick") {
                        appState.serverManager.kickClient(name: client.name)
                        selection = nil
                    }
                    .tint(.red)
                    .controlSize(.small)
                }

                Button {
                    appState.serverManager.fetchClients()
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .buttonStyle(.borderless)
                .controlSize(.small)
            }
            .padding(.horizontal)
            .padding(.vertical, 6)

            if appState.serverManager.clients.isEmpty {
                Text("No clients connected")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding()
            } else {
                Table(appState.serverManager.clients, selection: $selection) {
                    TableColumn("Name", value: \.name)
                        .width(min: 80, ideal: 120)
                    TableColumn("Address", value: \.addr)
                        .width(min: 100, ideal: 140)
                    TableColumn("Services", value: \.services)
                        .width(min: 100, ideal: 180)
                    TableColumn("Connected", value: \.connectedAt)
                        .width(min: 80, ideal: 140)
                }
            }
        }
    }
}
