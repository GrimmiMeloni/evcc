export default {
  header: {
    sessions: "Charging sessions",
    docs: "Documentation",
    blog: "Blog",
    github: "GitHub",
    login: "Vehicle logins",
    about: "About evcc",
    theme: {
      auto: "Design: system",
      light: "Design: light",
      dark: "Design: dark",
    },
  },
  footer: {
    version: {
      availableShort: "update",
      availableLong: "update available",
      modalTitle: "Update available",
      modalUpdateStarted: "Evcc will restart after the update..",
      modalInstalledVersion: "Currently installed version",
      modalNoReleaseNotes:
        "No release notes available. More information about the new version can be found here:",
      modalCancel: "Cancel",
      modalUpdate: "Update",
      modalUpdateNow: "Update now",
      modalDownload: "Download",
      modalUpdateStatusStart: "Update started: ",
      modalUpdateStatusFailed: "Update failed: ",
    },
    savings: {
      footerShort: "{percent}% solar",
      footerLong: "{percent}% solar energy",
      modalTitle: "Charge Energy Overview",
      sinceServerStart: "Since server start {since}.",
      percentTitle: "Solar Energy",
      percentSelf: "{self} kWh solar",
      percentGrid: "{grid} kWh grid",
      priceTitle: "Energy Price",
      priceFeedIn: "{feedInPrice} feed-in",
      priceGrid: "{gridPrice} grid",
      savingsTitle: "Savings",
      savingsComparedToGrid: "compared to grid",
      savingsTotalEnergy: "{total} kWh charged",
    },
    sponsor: {
      thanks: "Thanks for your support, {sponsor}! It helps us with the further development.",
      confetti: "Ready for confetti?",
      supportUs:
        "Our mission: Make solar charging the standard. Help us and support evcc financially.",
      sticker: "...or evcc stickers?",
      confettiPromise: "There will be stickers and digital confetti ;)",
      becomeSponsor: "Become a Sponsor",
    },
  },
  notifications: {
    modalTitle: "Notifications",
    dismissAll: "Dismiss all",
  },
  main: {
    energyflow: {
      noEnergy: "No meter data",
      homePower: "Consumption",
      pvProduction: "Production",
      loadpoints: "Loadpoint | Loadpoint | {count} Loadpoints",
      battery: "Battery",
      batteryCharge: "Battery charge",
      batteryDischarge: "Battery discharge",
      gridImport: "Grid import",
      selfConsumption: "Self consumption",
      pvExport: "Grid export",
    },
    mode: {
      off: "Off",
      minpv: "Min+PV",
      pv: "PV",
      now: "Fast",
    },
    loadpoint: {
      fallbackName: "Loadpoint",
      remoteDisabledSoft: "{source}: adaptive PV charging disabled",
      remoteDisabledHard: "{source}: disabled",
      power: "Power",
      charged: "Charged",
      duration: "Duration",
      remaining: "Remaining",
    },
    loadpointSettings: {
      title: 'Settings "{0}"',
      vehicle: "Vehicle",
      currents: "Charging",
      minSoC: {
        label: "Minimal SoC",
        description:
          'Range for emergencies. Vehicle gets "fast" charged to {0}% in PV mode. Then continues with PV surplus only.',
      },
      phasesConfigured: {
        label: "Phases",
        phases_0: "automatic switching",
        phases_1: "1 phase",
        phases_1_hint: "({min} to {max})",
        phases_3: "3 phases",
        phases_3_hint: "({min} to {max})",
      },
      maxCurrent: {
        label: "Max. Current",
      },
      minCurrent: {
        label: "Min. Current",
      },
      default: "default",
      disclaimerHint: "Note:",
      disclaimerText: "Changes are not persistent yet. They will be reset after server restart.",
    },
    vehicles: "Parking",
    vehicle: {
      fallbackName: "Vehicle",
      vehicleSoC: "SoC",
      targetSoC: "Limit",
      none: "No vehicle",
      unknown: "Guest vehicle",
      changeVehicle: "Change Vehicle",
      detectionActive: "Detecting vehicle ...",
    },
    vehicleSoC: {
      disconnected: "disconnected",
      charging: "charging",
      ready: "ready",
      connected: "connected",
      vehicleTarget: "Vehicle limit: {soc}%",
    },
    vehicleStatus: {
      minCharge: "minimum charging to {soc}%.",
      waitForVehicle: "Ready. Waiting for vehicle.",
      vehicleTargetReached: "Vehicle limit {soc}% reached.",
      charging: "Charging.",
      targetChargePlanned: "Target charge planned. Starting {time}.",
      targetChargeWaitForVehicle: "Target charge ready. Wait for vehicle.",
      targetChargeActive: "Target charge active.",
      connected: "Connected.",
      pvDisable: "Not enough surplus. Pausing in {remaining}.",
      pvEnable: "Surplus available. Starting in {remaining}.",
      scale1p: "Reduce to single phase in {remaining}.",
      scale3p: "Increase to three phase in {remaining}.",
      disconnected: "Disconnected.",
      unknown: "",
    },
    provider: {
      login: "login",
      logout: "logout",
    },
    targetCharge: {
      title: "Target Time",
      inactiveLabel: "Target time",
      activeLabel: "{time}",
      modalTitle: "Set Target Time",
      setTargetTime: "none",
      description: "When should the vehicle be charged to {targetSoC}%?",
      today: "today",
      tomorrow: "tomorrow",
      targetIsInThePast: "The chosen time is in the past.",
      remove: "Remove",
      activate: "Activate",
      experimentalLabel: "Experimental",
      experimentalText: `
        This feature works but isn't perfect yet.
        Please report unexpected behaviour in our
      `,
    },
    targetEnergy: {
      label: "Limit",
      noLimit: "none",
    },
  },
  startupError: {
    title: "Startup Error",
    description:
      "Please check your configuration file. If the error message does not help you, have a look at our {0}.",
    discussions: "GitHub Discussions",
    hint: "Note: Another reason why you see this message could be a faulty device (inverter, meter, ...). Check your network connections.",
    configuration: "Config",
    configFile: "Configuration file used:",
    lineError: "We found an error in {0}.",
    lineErrorLink: "line {0}",
    fixAndRestart: "Fix the problem and restart the server.",
    restartButton: "Restart",
  },
  sessions: {
    title: "Charging sessions",
    downloadCsv: "Download as CSV",
    loadpoint: "Loadpoint",
    vehicle: "Vehicle",
    energy: "Charged",
    date: "Finished",
  },
  offline: {
    message: "No connection to server.",
    reload: "Reload?",
  },
};
