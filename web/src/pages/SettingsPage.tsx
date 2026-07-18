import { useState } from "react";
import ToolsPanel from "../settings/ToolsPanel";
import DirsPanel from "../settings/DirsPanel";
import PasskeysPanel from "../settings/PasskeysPanel";
import AuthSessionsPanel from "../settings/AuthSessionsPanel";
import ServersPanel from "../settings/ServersPanel";
import DaemonPanel from "../settings/DaemonPanel";
import AppearancePanel from "../settings/AppearancePanel";

type Tab = "tools" | "dirs" | "passkeys" | "sessions" | "servers" | "daemon" | "appearance";

export default function SettingsPage() {
  const [activeTab, setActiveTab] = useState<Tab>("tools");

  const tabs: { id: Tab; label: string; component: React.ReactNode }[] = [
    { id: "tools", label: "Tools", component: <ToolsPanel /> },
    { id: "dirs", label: "Directories", component: <DirsPanel /> },
    { id: "passkeys", label: "Passkeys", component: <PasskeysPanel /> },
    { id: "sessions", label: "Sessions", component: <AuthSessionsPanel /> },
    { id: "servers", label: "Servers", component: <ServersPanel /> },
    { id: "daemon", label: "Daemon", component: <DaemonPanel /> },
    { id: "appearance", label: "Appearance", component: <AppearancePanel /> },
  ];

  return (
    <div className="settings-page">
      <h1>Settings</h1>
      <div className="tabs">
        <div className="tab-buttons">
          {tabs.map((tab) => (
            <button key={tab.id} onClick={() => setActiveTab(tab.id)} className={activeTab === tab.id ? "active" : ""}>
              {tab.label}
            </button>
          ))}
        </div>
        <div className="tab-content">{tabs.find((t) => t.id === activeTab)?.component}</div>
      </div>
    </div>
  );
}
