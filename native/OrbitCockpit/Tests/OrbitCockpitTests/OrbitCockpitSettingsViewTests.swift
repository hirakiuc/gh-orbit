import SwiftUI
import Testing

@testable import OrbitCockpit

@Suite("OrbitCockpit Settings View Tests")
@MainActor
struct OrbitCockpitSettingsViewTests {
    @Test("Settings view supports explicit section selection state")
    func testInitialSectionSelectionCanTargetAnySettingsTab() {
        for section in OrbitCockpitSettingsSection.allCases {
            let view = OrbitCockpitSettingsView(initialSection: section)
            #expect(view.selectedSection == section)
        }
    }

    @Test("Settings view keeps the wider minimum frame for tab labels")
    func testMinimumSettingsWindowFrame() {
        #expect(OrbitCockpitSettingsView.minimumWindowWidth == 700)
        #expect(OrbitCockpitSettingsView.minimumWindowHeight == 420)
    }
}
