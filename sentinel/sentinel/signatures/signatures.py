"""
Behavioral signature detection with MITRE ATT&CK mapping.

Categories:
- PERSISTENCE (T1547, T1053, T1543, T1546)
- DEFENSE_EVASION (T1562, T1055, T1070, T1027)
- CREDENTIAL_ACCESS (T1003, T1555, T1056)
- LATERAL_MOVEMENT (T1021, T1570)
- COMMAND_AND_CONTROL (T1071, T1572, T1573)
- EXFILTRATION (T1041, T1048, T1567)
- RANSOMWARE (T1486, T1490)
- AI_LLM_ATTACKS (custom)
"""

import re
from dataclasses import dataclass, field
from enum import Enum
from typing import Dict, List, Optional, Set, Tuple
from pathlib import Path


class SignatureCategory(Enum):
    """Signature categories aligned with MITRE ATT&CK tactics."""
    PERSISTENCE = "persistence"
    DEFENSE_EVASION = "defense_evasion"
    CREDENTIAL_ACCESS = "credential_access"
    LATERAL_MOVEMENT = "lateral_movement"
    COMMAND_AND_CONTROL = "command_and_control"
    EXFILTRATION = "exfiltration"
    RANSOMWARE = "ransomware"
    AI_LLM_ATTACKS = "ai_llm_attacks"
    EXECUTION = "execution"
    DISCOVERY = "discovery"


@dataclass
class Signature:
    """A behavioral detection signature."""
    name: str
    pattern: str
    severity: str  # low, medium, high, critical
    category: SignatureCategory
    description: str
    mitre_id: Optional[str] = None
    mitre_technique: Optional[str] = None
    tags: List[str] = field(default_factory=list)
    case_sensitive: bool = False
    
    def __post_init__(self):
        flags = 0 if self.case_sensitive else re.IGNORECASE
        self._compiled = re.compile(self.pattern, flags | re.MULTILINE)
    
    def match(self, content: str) -> List[Tuple[int, int, str]]:
        """Return list of (start, end, matched_text) tuples."""
        matches = []
        for m in self._compiled.finditer(content):
            matches.append((m.start(), m.end(), m.group()))
        return matches


@dataclass
class SignatureMatch:
    """A signature match with context."""
    signature: Signature
    matched_text: str
    offset: int
    line_number: int
    context: str  # Surrounding lines
    
    def to_dict(self) -> Dict:
        return {
            "name": self.signature.name,
            "category": self.signature.category.value,
            "severity": self.signature.severity,
            "mitre_id": self.signature.mitre_id,
            "matched_text": self.matched_text[:200],
            "line_number": self.line_number,
            "context": self.context[:500],
        }


# =============================================================================
# PERSISTENCE SIGNATURES (T1547, T1053, T1543, T1546)
# =============================================================================

PERSISTENCE_SIGNATURES = [
    # Registry Run Keys
    Signature(
        name="registry_run_key",
        pattern=r"(HKLM|HKCU|HKEY_LOCAL_MACHINE|HKEY_CURRENT_USER)[\\\/]+(Software[\\\/]+Microsoft[\\\/]+Windows[\\\/]+CurrentVersion[\\\/]+(Run|RunOnce|RunServices|RunServicesOnce))",
        severity="high",
        category=SignatureCategory.PERSISTENCE,
        description="Registry run key for persistence",
        mitre_id="T1547.001",
        mitre_technique="Boot or Logon Autostart Execution: Registry Run Keys",
        tags=["persistence", "registry", "autostart"],
    ),
    Signature(
        name="registry_winlogon",
        pattern=r"(HKLM|HKEY_LOCAL_MACHINE)[\\\/]+Software[\\\/]+Microsoft[\\\/]+Windows NT[\\\/]+CurrentVersion[\\\/]+Winlogon[\\\/]*(Shell|Userinit|Notify)",
        severity="critical",
        category=SignatureCategory.PERSISTENCE,
        description="Winlogon registry modification for persistence",
        mitre_id="T1547.004",
        mitre_technique="Boot or Logon Autostart Execution: Winlogon Helper DLL",
        tags=["persistence", "registry", "winlogon"],
    ),
    # Scheduled Tasks
    Signature(
        name="schtasks_create",
        pattern=r"schtasks\s*(/create|\.exe.*create)\s+.*(\/sc|\/tn|\/tr)",
        severity="high",
        category=SignatureCategory.PERSISTENCE,
        description="Scheduled task creation",
        mitre_id="T1053.005",
        mitre_technique="Scheduled Task/Job: Scheduled Task",
        tags=["persistence", "schtasks"],
    ),
    Signature(
        name="at_command",
        pattern=r"\bat\s+\d{1,2}:\d{2}\s+",
        severity="medium",
        category=SignatureCategory.PERSISTENCE,
        description="AT command for scheduled execution",
        mitre_id="T1053.002",
        mitre_technique="Scheduled Task/Job: At",
        tags=["persistence", "at_command"],
    ),
    # Startup Folder
    Signature(
        name="startup_folder",
        pattern=r"(Startup|Start Menu[\\\/]+Programs[\\\/]+Startup|AppData[\\\/]+Roaming[\\\/]+Microsoft[\\\/]+Windows[\\\/]+Start Menu[\\\/]+Programs[\\\/]+Startup)",
        severity="high",
        category=SignatureCategory.PERSISTENCE,
        description="Startup folder access for persistence",
        mitre_id="T1547.001",
        mitre_technique="Boot or Logon Autostart Execution: Registry Run Keys",
        tags=["persistence", "startup"],
    ),
    # Services
    Signature(
        name="sc_create_service",
        pattern=r"sc\s+(create|config)\s+\w+\s+(binpath|start|type)=",
        severity="high",
        category=SignatureCategory.PERSISTENCE,
        description="Windows service creation/modification",
        mitre_id="T1543.003",
        mitre_technique="Create or Modify System Process: Windows Service",
        tags=["persistence", "service"],
    ),
    Signature(
        name="new_service_powershell",
        pattern=r"New-Service\s+.*-BinaryPathName",
        severity="high",
        category=SignatureCategory.PERSISTENCE,
        description="PowerShell service creation",
        mitre_id="T1543.003",
        mitre_technique="Create or Modify System Process: Windows Service",
        tags=["persistence", "service", "powershell"],
    ),
    # WMI Subscriptions
    Signature(
        name="wmi_subscription",
        pattern=r"(__EventFilter|__EventConsumer|__FilterToConsumerBinding|ActiveScriptEventConsumer|CommandLineEventConsumer)",
        severity="critical",
        category=SignatureCategory.PERSISTENCE,
        description="WMI event subscription for persistence",
        mitre_id="T1546.003",
        mitre_technique="Event Triggered Execution: WMI Event Subscription",
        tags=["persistence", "wmi"],
    ),
    Signature(
        name="wmic_process_call",
        pattern=r"wmic\s+.*process\s+call\s+create",
        severity="high",
        category=SignatureCategory.PERSISTENCE,
        description="WMI process creation",
        mitre_id="T1047",
        mitre_technique="Windows Management Instrumentation",
        tags=["persistence", "wmi", "execution"],
    ),
    # DLL Hijacking
    Signature(
        name="dll_hijack_path",
        pattern=r"(System32|SysWOW64|Windows)[\\\/]+(version|dwmapi|cryptbase|cryptsp|bcrypt|ntmarta|wer|WLDP)\.dll",
        severity="high",
        category=SignatureCategory.PERSISTENCE,
        description="Known DLL hijacking target",
        mitre_id="T1574.001",
        mitre_technique="Hijack Execution Flow: DLL Search Order Hijacking",
        tags=["persistence", "dll_hijack"],
    ),
    Signature(
        name="dll_sideloading",
        pattern=r"(LoadLibrary|LoadLibraryEx|LoadModule)\s*\(\s*[\"'].*\.dll[\"']",
        severity="medium",
        category=SignatureCategory.PERSISTENCE,
        description="Dynamic DLL loading (potential sideloading)",
        mitre_id="T1574.002",
        mitre_technique="Hijack Execution Flow: DLL Side-Loading",
        tags=["persistence", "dll_sideload"],
    ),
    # Boot Records
    Signature(
        name="mbr_access",
        pattern=r"(\\\\.\\\\PhysicalDrive0|\\\\.\\\\PHYSICALDRIVE0|CreateFile.*PhysicalDrive)",
        severity="critical",
        category=SignatureCategory.PERSISTENCE,
        description="Direct disk access (potential bootkit)",
        mitre_id="T1542.003",
        mitre_technique="Pre-OS Boot: Bootkit",
        tags=["persistence", "bootkit", "mbr"],
    ),
    Signature(
        name="vbr_modification",
        pattern=r"(bootsect|bcdedit|bootmgr|boot\.ini)",
        severity="high",
        category=SignatureCategory.PERSISTENCE,
        description="Boot configuration modification",
        mitre_id="T1542",
        mitre_technique="Pre-OS Boot",
        tags=["persistence", "boot"],
    ),
    # COM Hijacking
    Signature(
        name="com_hijacking",
        pattern=r"(HKCU|HKLM)[\\\/]+Software[\\\/]+Classes[\\\/]+CLSID[\\\/]+\{[0-9a-fA-F-]+\}[\\\/]+InprocServer32",
        severity="high",
        category=SignatureCategory.PERSISTENCE,
        description="COM object hijacking",
        mitre_id="T1546.015",
        mitre_technique="Event Triggered Execution: COM Hijacking",
        tags=["persistence", "com"],
    ),
    # AppInit DLLs
    Signature(
        name="appinit_dlls",
        pattern=r"(HKLM|HKEY_LOCAL_MACHINE)[\\\/]+Software[\\\/]+Microsoft[\\\/]+Windows NT[\\\/]+CurrentVersion[\\\/]+Windows[\\\/]+AppInit_DLLs",
        severity="critical",
        category=SignatureCategory.PERSISTENCE,
        description="AppInit_DLLs registry modification",
        mitre_id="T1546.010",
        mitre_technique="Event Triggered Execution: AppInit DLLs",
        tags=["persistence", "appinit"],
    ),
    # Image File Execution Options
    Signature(
        name="ifeo_debugger",
        pattern=r"(HKLM|HKEY_LOCAL_MACHINE)[\\\/]+Software[\\\/]+Microsoft[\\\/]+Windows NT[\\\/]+CurrentVersion[\\\/]+Image File Execution Options[\\\/]+.*[\\\/]+Debugger",
        severity="critical",
        category=SignatureCategory.PERSISTENCE,
        description="IFEO debugger persistence",
        mitre_id="T1546.012",
        mitre_technique="Event Triggered Execution: IFEO Injection",
        tags=["persistence", "ifeo"],
    ),
]

# =============================================================================
# DEFENSE EVASION SIGNATURES (T1562, T1055, T1070, T1027)
# =============================================================================

DEFENSE_EVASION_SIGNATURES = [
    # Disable AV/EDR
    Signature(
        name="disable_defender",
        pattern=r"(Set-MpPreference\s+.*-Disable|DisableRealtimeMonitoring|DisableBehaviorMonitoring|DisableIOAVProtection|DisableScriptScanning)",
        severity="critical",
        category=SignatureCategory.DEFENSE_EVASION,
        description="Windows Defender tampering",
        mitre_id="T1562.001",
        mitre_technique="Impair Defenses: Disable or Modify Tools",
        tags=["evasion", "defender", "av_disable"],
    ),
    Signature(
        name="stop_av_service",
        pattern=r"(net\s+stop|sc\s+stop|Stop-Service)\s+.*(WinDefend|MsMpSvc|Sense|SecurityHealthService|wscsvc|WdNisSvc)",
        severity="critical",
        category=SignatureCategory.DEFENSE_EVASION,
        description="Stopping security services",
        mitre_id="T1562.001",
        mitre_technique="Impair Defenses: Disable or Modify Tools",
        tags=["evasion", "av_disable"],
    ),
    Signature(
        name="av_exclusion",
        pattern=r"(Add-MpPreference\s+.*-Exclusion|ExclusionPath|ExclusionExtension|ExclusionProcess)",
        severity="high",
        category=SignatureCategory.DEFENSE_EVASION,
        description="Adding AV exclusions",
        mitre_id="T1562.001",
        mitre_technique="Impair Defenses: Disable or Modify Tools",
        tags=["evasion", "av_exclusion"],
    ),
    # Process Hollowing/Injection
    Signature(
        name="process_hollowing",
        pattern=r"(NtUnmapViewOfSection|ZwUnmapViewOfSection|NtWriteVirtualMemory|WriteProcessMemory.*VirtualAllocEx.*CreateRemoteThread)",
        severity="critical",
        category=SignatureCategory.DEFENSE_EVASION,
        description="Process hollowing indicators",
        mitre_id="T1055.012",
        mitre_technique="Process Injection: Process Hollowing",
        tags=["evasion", "injection", "hollowing"],
    ),
    Signature(
        name="dll_injection",
        pattern=r"(VirtualAllocEx.*WriteProcessMemory.*CreateRemoteThread|NtCreateThreadEx|RtlCreateUserThread)",
        severity="critical",
        category=SignatureCategory.DEFENSE_EVASION,
        description="DLL injection pattern",
        mitre_id="T1055.001",
        mitre_technique="Process Injection: DLL Injection",
        tags=["evasion", "injection", "dll"],
    ),
    Signature(
        name="apc_injection",
        pattern=r"(QueueUserAPC|NtQueueApcThread)",
        severity="high",
        category=SignatureCategory.DEFENSE_EVASION,
        description="APC injection technique",
        mitre_id="T1055.004",
        mitre_technique="Process Injection: APC Injection",
        tags=["evasion", "injection", "apc"],
    ),
    # Timestomping
    Signature(
        name="timestomp",
        pattern=r"(SetFileTime|NtSetInformationFile.*FileBasicInformation|\$_.CreationTime|\$_.LastWriteTime|\$_.LastAccessTime\s*=)",
        severity="high",
        category=SignatureCategory.DEFENSE_EVASION,
        description="File timestamp manipulation",
        mitre_id="T1070.006",
        mitre_technique="Indicator Removal: Timestomp",
        tags=["evasion", "timestomp"],
    ),
    # Log Clearing
    Signature(
        name="clear_eventlog",
        pattern=r"(wevtutil\s+(cl|clear-log)|Clear-EventLog|Remove-EventLog|for\s+\/F.*wevtutil\s+cl)",
        severity="critical",
        category=SignatureCategory.DEFENSE_EVASION,
        description="Event log clearing",
        mitre_id="T1070.001",
        mitre_technique="Indicator Removal: Clear Windows Event Logs",
        tags=["evasion", "log_clear"],
    ),
    Signature(
        name="delete_logs",
        pattern=r"(del|Remove-Item|rm)\s+.*\.(log|evtx|evt)\b",
        severity="high",
        category=SignatureCategory.DEFENSE_EVASION,
        description="Log file deletion",
        mitre_id="T1070.001",
        mitre_technique="Indicator Removal: Clear Windows Event Logs",
        tags=["evasion", "log_delete"],
    ),
    # AMSI Bypass
    Signature(
        name="amsi_bypass",
        pattern=r"(AmsiScanBuffer|amsiInitFailed|AmsiUtils|amsi\.dll|AmsiContext|AmsiOpenSession)",
        severity="critical",
        category=SignatureCategory.DEFENSE_EVASION,
        description="AMSI bypass attempt",
        mitre_id="T1562.001",
        mitre_technique="Impair Defenses: Disable or Modify Tools",
        tags=["evasion", "amsi"],
    ),
    Signature(
        name="amsi_patch",
        pattern=r"(\[Runtime\.InteropServices\.Marshal\]::Copy\s*\(\s*\$\w+,\s*0,\s*\$\w+,\s*\d+\)|amsi.*patch|patch.*amsi)",
        severity="critical",
        category=SignatureCategory.DEFENSE_EVASION,
        description="AMSI memory patching",
        mitre_id="T1562.001",
        mitre_technique="Impair Defenses: Disable or Modify Tools",
        tags=["evasion", "amsi", "patch"],
    ),
    # ETW Bypass
    Signature(
        name="etw_bypass",
        pattern=r"(EtwEventWrite|NtTraceEvent|EtwpEventWriteFull|EventWrite\s*=|etw.*patch|patch.*etw)",
        severity="critical",
        category=SignatureCategory.DEFENSE_EVASION,
        description="ETW bypass/patching",
        mitre_id="T1562.006",
        mitre_technique="Impair Defenses: Indicator Blocking",
        tags=["evasion", "etw"],
    ),
    # Obfuscation Patterns
    Signature(
        name="base64_powershell",
        pattern=r"powershell.*-e(nc(odedcommand)?|c)\s+[A-Za-z0-9+/=]{50,}",
        severity="high",
        category=SignatureCategory.DEFENSE_EVASION,
        description="Base64 encoded PowerShell command",
        mitre_id="T1027",
        mitre_technique="Obfuscated Files or Information",
        tags=["evasion", "obfuscation", "base64"],
    ),
    Signature(
        name="string_concat_obfuscation",
        pattern=r"(\$\w+\s*\+\s*){5,}|\(\s*['\"][^'\"]{1,3}['\"]\s*\+\s*['\"][^'\"]{1,3}['\"]\s*\){3,}",
        severity="medium",
        category=SignatureCategory.DEFENSE_EVASION,
        description="String concatenation obfuscation",
        mitre_id="T1027",
        mitre_technique="Obfuscated Files or Information",
        tags=["evasion", "obfuscation"],
    ),
    Signature(
        name="char_code_obfuscation",
        pattern=r"(\[char\]\d+\s*\+\s*){3,}|(\[char\]\(\d+\)\s*){5,}",
        severity="high",
        category=SignatureCategory.DEFENSE_EVASION,
        description="Character code obfuscation",
        mitre_id="T1027",
        mitre_technique="Obfuscated Files or Information",
        tags=["evasion", "obfuscation", "charcode"],
    ),
    # Disable Firewall
    Signature(
        name="disable_firewall",
        pattern=r"(netsh\s+.*firewall\s+.*disable|Set-NetFirewallProfile.*-Enabled\s+False|netsh\s+advfirewall\s+set\s+.*state\s+off)",
        severity="high",
        category=SignatureCategory.DEFENSE_EVASION,
        description="Firewall disabled",
        mitre_id="T1562.004",
        mitre_technique="Impair Defenses: Disable or Modify Firewall",
        tags=["evasion", "firewall"],
    ),
    # Unhooking
    Signature(
        name="unhook_ntdll",
        pattern=r"(ntdll.*copy|fresh.*ntdll|map.*ntdll.*section|NtProtectVirtualMemory.*PAGE_EXECUTE_READWRITE)",
        severity="critical",
        category=SignatureCategory.DEFENSE_EVASION,
        description="NTDLL unhooking attempt",
        mitre_id="T1562.001",
        mitre_technique="Impair Defenses: Disable or Modify Tools",
        tags=["evasion", "unhook"],
    ),
]

# =============================================================================
# CREDENTIAL ACCESS SIGNATURES (T1003, T1555, T1056)
# =============================================================================

CREDENTIAL_ACCESS_SIGNATURES = [
    # Mimikatz
    Signature(
        name="mimikatz_command",
        pattern=r"(sekurlsa::|kerberos::|lsadump::|privilege::debug|token::elevate|crypto::capi)",
        severity="critical",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="Mimikatz command pattern",
        mitre_id="T1003.001",
        mitre_technique="OS Credential Dumping: LSASS Memory",
        tags=["credential", "mimikatz"],
    ),
    Signature(
        name="mimikatz_strings",
        pattern=r"(gentilkiwi|mimilib|mimidrv|mimikatz|dpapi::|vault::cred|logonpasswords)",
        severity="critical",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="Mimikatz tool indicators",
        mitre_id="T1003.001",
        mitre_technique="OS Credential Dumping: LSASS Memory",
        tags=["credential", "mimikatz"],
    ),
    # LSASS Access
    Signature(
        name="lsass_dump",
        pattern=r"(procdump.*lsass|rundll32.*comsvcs.*MiniDump.*lsass|tasklist.*lsass|Get-Process.*lsass)",
        severity="critical",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="LSASS memory dump attempt",
        mitre_id="T1003.001",
        mitre_technique="OS Credential Dumping: LSASS Memory",
        tags=["credential", "lsass"],
    ),
    Signature(
        name="lsass_handle",
        pattern=r"(OpenProcess.*lsass|PROCESS_VM_READ.*lsass|MiniDumpWriteDump.*lsass)",
        severity="critical",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="LSASS process handle access",
        mitre_id="T1003.001",
        mitre_technique="OS Credential Dumping: LSASS Memory",
        tags=["credential", "lsass"],
    ),
    # SAM/SYSTEM Dump
    Signature(
        name="sam_dump",
        pattern=r"(reg\s+save\s+.*(sam|system|security)|copy.*\\config\\(sam|system|security)|vssadmin.*shadow.*sam)",
        severity="critical",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="SAM/SYSTEM registry dump",
        mitre_id="T1003.002",
        mitre_technique="OS Credential Dumping: Security Account Manager",
        tags=["credential", "sam"],
    ),
    Signature(
        name="ntds_dump",
        pattern=r"(ntdsutil.*ifm|vssadmin.*create\s+shadow.*ntds|copy.*ntds\.dit|esentutl.*ntds)",
        severity="critical",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="NTDS.dit credential extraction",
        mitre_id="T1003.003",
        mitre_technique="OS Credential Dumping: NTDS",
        tags=["credential", "ntds", "ad"],
    ),
    # Credential Files
    Signature(
        name="credential_file_access",
        pattern=r"(Credentials\\|Vault\\|Microsoft\.CRM|\\AppData\\.*\\Google\\Chrome\\User Data\\Default\\Login Data|\\.ssh\\|id_rsa|id_dsa|\.pgpass|\.netrc)",
        severity="high",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="Credential file access",
        mitre_id="T1555",
        mitre_technique="Credentials from Password Stores",
        tags=["credential", "file"],
    ),
    # Browser Credentials
    Signature(
        name="browser_cred_access",
        pattern=r"(Login Data|logins\.json|signons\.sqlite|cookies\.sqlite|Web Data|Chrome\\User Data|Firefox\\Profiles)",
        severity="high",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="Browser credential file access",
        mitre_id="T1555.003",
        mitre_technique="Credentials from Web Browsers",
        tags=["credential", "browser"],
    ),
    Signature(
        name="dpapi_decrypt",
        pattern=r"(CryptUnprotectData|CRYPTPROTECT_UI_FORBIDDEN|Dpapi|ProtectedData\.Unprotect)",
        severity="high",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="DPAPI credential decryption",
        mitre_id="T1555",
        mitre_technique="Credentials from Password Stores",
        tags=["credential", "dpapi"],
    ),
    # Keylogger
    Signature(
        name="keylogger_api",
        pattern=r"(SetWindowsHookEx.*WH_KEYBOARD|GetAsyncKeyState|GetKeyState|GetKeyboardState|RegisterRawInputDevices)",
        severity="high",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="Keylogger API calls",
        mitre_id="T1056.001",
        mitre_technique="Input Capture: Keylogging",
        tags=["credential", "keylogger"],
    ),
    # DCSync
    Signature(
        name="dcsync_attack",
        pattern=r"(DRS.*GetNCChanges|lsadump::dcsync|DS-Replication-Get-Changes|1131f6aa-9c07-11d1-f79f-00c04fc2dcd2)",
        severity="critical",
        category=SignatureCategory.CREDENTIAL_ACCESS,
        description="DCSync attack indicators",
        mitre_id="T1003.006",
        mitre_technique="OS Credential Dumping: DCSync",
        tags=["credential", "dcsync", "ad"],
    ),
]

# =============================================================================
# LATERAL MOVEMENT SIGNATURES (T1021, T1570)
# =============================================================================

LATERAL_MOVEMENT_SIGNATURES = [
    # PsExec
    Signature(
        name="psexec_execution",
        pattern=r"(psexec(64)?\.exe|psexesvc|\\\\.*\\ADMIN\$|\\\\.*\\C\$.*\.exe|\\\\.*\\IPC\$)",
        severity="high",
        category=SignatureCategory.LATERAL_MOVEMENT,
        description="PsExec remote execution",
        mitre_id="T1021.002",
        mitre_technique="Remote Services: SMB/Windows Admin Shares",
        tags=["lateral", "psexec"],
    ),
    Signature(
        name="remote_service_creation",
        pattern=r"(sc\s+\\\\.*\s+create|sc\s+\\\\.*\s+start|sc\.exe.*\\\\)",
        severity="high",
        category=SignatureCategory.LATERAL_MOVEMENT,
        description="Remote service creation",
        mitre_id="T1021.002",
        mitre_technique="Remote Services: SMB/Windows Admin Shares",
        tags=["lateral", "service"],
    ),
    # WMI
    Signature(
        name="wmi_remote",
        pattern=r"(wmic\s+/node:|Invoke-WmiMethod.*-ComputerName|Get-WmiObject.*-ComputerName|New-CimSession)",
        severity="high",
        category=SignatureCategory.LATERAL_MOVEMENT,
        description="WMI remote execution",
        mitre_id="T1021.003",
        mitre_technique="Remote Services: WMI",
        tags=["lateral", "wmi"],
    ),
    # WinRM
    Signature(
        name="winrm_execution",
        pattern=r"(Invoke-Command\s+.*-ComputerName|Enter-PSSession|New-PSSession|winrm\s+.*quickconfig|Enable-PSRemoting)",
        severity="high",
        category=SignatureCategory.LATERAL_MOVEMENT,
        description="WinRM/PowerShell remoting",
        mitre_id="T1021.006",
        mitre_technique="Remote Services: Windows Remote Management",
        tags=["lateral", "winrm"],
    ),
    # RDP
    Signature(
        name="rdp_enable",
        pattern=r"(fDenyTSConnections.*0|Enable-NetFirewallRule.*Remote Desktop|netsh.*firewall.*remotedesktop|mstsc.*\/v:)",
        severity="medium",
        category=SignatureCategory.LATERAL_MOVEMENT,
        description="RDP enablement/usage",
        mitre_id="T1021.001",
        mitre_technique="Remote Services: RDP",
        tags=["lateral", "rdp"],
    ),
    Signature(
        name="rdp_tunneling",
        pattern=r"(plink.*-L.*3389|ssh.*-L.*3389|netsh.*portproxy.*3389)",
        severity="high",
        category=SignatureCategory.LATERAL_MOVEMENT,
        description="RDP tunneling",
        mitre_id="T1021.001",
        mitre_technique="Remote Services: RDP",
        tags=["lateral", "rdp", "tunnel"],
    ),
    # SMB
    Signature(
        name="smb_admin_share",
        pattern=r"(net\s+use\s+\\\\|New-PSDrive.*\\\\|copy.*\\\\.*\\(C|ADMIN)\$)",
        severity="high",
        category=SignatureCategory.LATERAL_MOVEMENT,
        description="SMB admin share access",
        mitre_id="T1021.002",
        mitre_technique="Remote Services: SMB/Windows Admin Shares",
        tags=["lateral", "smb"],
    ),
    # Pass-the-Hash
    Signature(
        name="pass_the_hash",
        pattern=r"(sekurlsa::pth|Invoke-Mimikatz.*pth|Invoke-SMBExec|Invoke-WMIExec.*-Hash|/ntlm:)",
        severity="critical",
        category=SignatureCategory.LATERAL_MOVEMENT,
        description="Pass-the-hash attack",
        mitre_id="T1550.002",
        mitre_technique="Use Alternate Authentication Material: Pass the Hash",
        tags=["lateral", "pth"],
    ),
    # Pass-the-Ticket
    Signature(
        name="pass_the_ticket",
        pattern=r"(kerberos::ptt|Invoke-Mimikatz.*ptt|Rubeus.*ptt|\.kirbi)",
        severity="critical",
        category=SignatureCategory.LATERAL_MOVEMENT,
        description="Pass-the-ticket attack",
        mitre_id="T1550.003",
        mitre_technique="Use Alternate Authentication Material: Pass the Ticket",
        tags=["lateral", "ptt", "kerberos"],
    ),
    # SSH
    Signature(
        name="ssh_lateral",
        pattern=r"(ssh\s+.*@|scp\s+.*:|plink\s+.*-pw|putty.*-pw)",
        severity="medium",
        category=SignatureCategory.LATERAL_MOVEMENT,
        description="SSH-based lateral movement",
        mitre_id="T1021.004",
        mitre_technique="Remote Services: SSH",
        tags=["lateral", "ssh"],
    ),
]

# =============================================================================
# COMMAND AND CONTROL SIGNATURES (T1071, T1572, T1573)
# =============================================================================

C2_SIGNATURES = [
    # Base64 PowerShell
    Signature(
        name="encoded_powershell",
        pattern=r"powershell.*(-e|-enc|-encodedcommand)\s+([A-Za-z0-9+/=]{40,}|\$\w+)",
        severity="high",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Encoded PowerShell execution",
        mitre_id="T1059.001",
        mitre_technique="Command and Scripting Interpreter: PowerShell",
        tags=["c2", "powershell", "encoded"],
    ),
    Signature(
        name="base64_payload_variable",
        pattern=r"\$\w+\s*=\s*[\"'][A-Za-z0-9+/=]{50,}[\"']",
        severity="high",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Base64 payload stored in variable",
        mitre_id="T1027",
        mitre_technique="Obfuscated Files or Information",
        tags=["c2", "base64", "encoded"],
    ),
    # DNS Tunneling
    Signature(
        name="dns_tunneling",
        pattern=r"(nslookup\s+.*-q=txt|Resolve-DnsName.*-Type TXT|iodine|dnscat|dns2tcp|\.(ns|txt)\.[a-z0-9]{20,}\.)",
        severity="high",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="DNS tunneling indicators",
        mitre_id="T1071.004",
        mitre_technique="Application Layer Protocol: DNS",
        tags=["c2", "dns", "tunnel"],
    ),
    Signature(
        name="dns_exfil_pattern",
        pattern=r"[a-z0-9]{32,}\.[a-z0-9-]+\.(com|net|org|io)",
        severity="medium",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Potential DNS exfiltration subdomain",
        mitre_id="T1071.004",
        mitre_technique="Application Layer Protocol: DNS",
        tags=["c2", "dns", "exfil"],
    ),
    # ICMP Tunneling
    Signature(
        name="icmp_tunnel",
        pattern=r"(ptunnel|icmpsh|icmptunnel|Hans\s+ICMP|ping\s+.*-t.*-l\s+\d{4,})",
        severity="high",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="ICMP tunneling indicators",
        mitre_id="T1095",
        mitre_technique="Non-Application Layer Protocol",
        tags=["c2", "icmp", "tunnel"],
    ),
    # HTTP Beaconing
    Signature(
        name="http_beacon_pattern",
        pattern=r"(Invoke-WebRequest.*-Method\s+POST.*-Body|WebClient.*UploadString|HttpClient.*PostAsync|curl.*-X\s+POST.*-d)",
        severity="medium",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="HTTP POST beaconing pattern",
        mitre_id="T1071.001",
        mitre_technique="Application Layer Protocol: Web Protocols",
        tags=["c2", "http", "beacon"],
    ),
    Signature(
        name="user_agent_anomaly",
        pattern=r"(User-Agent|UserAgent)\s*[=:]\s*[\"']?(Mozilla\/4\.0|MSIE\s*6|curl\/|wget\/|python-requests|Go-http-client)",
        severity="low",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Suspicious user agent string",
        mitre_id="T1071.001",
        mitre_technique="Application Layer Protocol: Web Protocols",
        tags=["c2", "http", "useragent"],
    ),
    # Known C2 Frameworks
    Signature(
        name="cobalt_strike",
        pattern=r"(beacon\.dll|beacon\.exe|cobaltstrike|\.cs\s+profile|sleeptime|jitter|spawn(to|as)|pipename)",
        severity="critical",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Cobalt Strike indicators",
        mitre_id="S0154",
        mitre_technique="Cobalt Strike",
        tags=["c2", "cobaltstrike"],
    ),
    Signature(
        name="metasploit_pattern",
        pattern=r"(meterpreter|msfvenom|msf::|reverse_tcp|bind_tcp|shikata_ga_nai|x86/call4_dword_xor)",
        severity="critical",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Metasploit indicators",
        mitre_id="S0081",
        mitre_technique="Metasploit",
        tags=["c2", "metasploit"],
    ),
    Signature(
        name="empire_pattern",
        pattern=r"(Empire\s+agent|Invoke-Empire|powershell.*empire|stager|launcher\.bat|listener.*http)",
        severity="critical",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Empire C2 indicators",
        mitre_id="S0363",
        mitre_technique="Empire",
        tags=["c2", "empire"],
    ),
    # Reverse Shells
    Signature(
        name="reverse_shell_bash",
        pattern=r"(bash\s+-i\s+>&\s*/dev/tcp/|nc\s+-e\s+/bin/(ba)?sh|mkfifo\s+/tmp/.*nc|/dev/tcp/\d+\.\d+\.\d+\.\d+/\d+)",
        severity="critical",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Bash reverse shell",
        mitre_id="T1059.004",
        mitre_technique="Command and Scripting Interpreter: Unix Shell",
        tags=["c2", "reverse_shell", "bash"],
    ),
    Signature(
        name="reverse_shell_powershell",
        pattern=r"(TCPClient\s*\(\s*[\"'][^\"']+[\"']\s*,\s*\d+|New-Object\s+.*Net\.Sockets\.TCPClient|Invoke-PowerShellTcp|powercat)",
        severity="critical",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="PowerShell reverse shell",
        mitre_id="T1059.001",
        mitre_technique="Command and Scripting Interpreter: PowerShell",
        tags=["c2", "reverse_shell", "powershell"],
    ),
    Signature(
        name="reverse_shell_python",
        pattern=r"(socket\.socket.*connect.*subprocess|pty\.spawn|os\.dup2\(s\.fileno|python.*-c.*import socket)",
        severity="critical",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Python reverse shell",
        mitre_id="T1059.006",
        mitre_technique="Command and Scripting Interpreter: Python",
        tags=["c2", "reverse_shell", "python"],
    ),
    # Domain Fronting
    Signature(
        name="domain_fronting",
        pattern=r"(Host:\s*[\w.-]+\.cloudfront\.net|Host:\s*[\w.-]+\.azureedge\.net|Host:\s*[\w.-]+\.fastly\.net|cloudfunctions\.net)",
        severity="high",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Domain fronting indicators",
        mitre_id="T1090.004",
        mitre_technique="Proxy: Domain Fronting",
        tags=["c2", "domain_fronting"],
    ),
    # Sliver C2
    Signature(
        name="sliver_c2",
        pattern=r"(sliver|implant|mtls|wg-keygen|generate.*--mtls|sessions\s+-i)",
        severity="critical",
        category=SignatureCategory.COMMAND_AND_CONTROL,
        description="Sliver C2 indicators",
        mitre_id="S0633",
        mitre_technique="Sliver",
        tags=["c2", "sliver"],
    ),
]

# =============================================================================
# EXFILTRATION SIGNATURES (T1041, T1048, T1567)
# =============================================================================

EXFILTRATION_SIGNATURES = [
    # Archive + Encrypt
    Signature(
        name="archive_sensitive",
        pattern=r"(Compress-Archive|7z\s+a.*-p|zip.*-e|rar\s+a.*-hp|tar.*-cz).*(\\.doc|\\.xls|\\.pdf|\\.pst|\\.db)",
        severity="high",
        category=SignatureCategory.EXFILTRATION,
        description="Archiving sensitive files",
        mitre_id="T1560.001",
        mitre_technique="Archive Collected Data: Archive via Utility",
        tags=["exfil", "archive"],
    ),
    Signature(
        name="password_protected_archive",
        pattern=r"(7z.*-p['\"]?\w+|zip.*-P\s*\w+|rar.*-hp\w+|gpg.*-c)",
        severity="high",
        category=SignatureCategory.EXFILTRATION,
        description="Password-protected archive creation",
        mitre_id="T1560.001",
        mitre_technique="Archive Collected Data: Archive via Utility",
        tags=["exfil", "archive", "encrypted"],
    ),
    # Cloud Upload
    Signature(
        name="cloud_exfil",
        pattern=r"(rclone\s+copy|rclone\s+sync|aws\s+s3\s+cp|gsutil\s+cp|azcopy|mega-put|dropbox.*upload)",
        severity="high",
        category=SignatureCategory.EXFILTRATION,
        description="Cloud storage exfiltration",
        mitre_id="T1567.002",
        mitre_technique="Exfiltration to Cloud Storage",
        tags=["exfil", "cloud"],
    ),
    Signature(
        name="file_sharing_upload",
        pattern=r"(pastebin\.com|transfer\.sh|file\.io|wetransfer|sendspace|mediafire|mega\.nz|gofile\.io)",
        severity="medium",
        category=SignatureCategory.EXFILTRATION,
        description="File sharing service usage",
        mitre_id="T1567",
        mitre_technique="Exfiltration Over Web Service",
        tags=["exfil", "sharing"],
    ),
    # DNS Exfil
    Signature(
        name="dns_data_exfil",
        pattern=r"(Out-DNSExfiltration|Invoke-DNSExfiltration|dnsexfil|dns.*exfil|dnsteal)",
        severity="critical",
        category=SignatureCategory.EXFILTRATION,
        description="DNS data exfiltration",
        mitre_id="T1048.003",
        mitre_technique="Exfiltration Over Alternative Protocol: Exfiltration Over DNS",
        tags=["exfil", "dns"],
    ),
    # Chunked Transfer
    Signature(
        name="chunked_exfil",
        pattern=r"(split\s+-b|Get-Content.*-ReadCount|\.Substring\(\d+,\s*\d+\)|chunk|fragment.*send)",
        severity="medium",
        category=SignatureCategory.EXFILTRATION,
        description="Chunked data transfer pattern",
        mitre_id="T1030",
        mitre_technique="Data Transfer Size Limits",
        tags=["exfil", "chunk"],
    ),
    # Steganography
    Signature(
        name="steganography",
        pattern=r"(steghide|openstego|outguess|stegano|LSBSteg|Invoke-PSImage|Out-ImageData)",
        severity="high",
        category=SignatureCategory.EXFILTRATION,
        description="Steganography tool usage",
        mitre_id="T1027.003",
        mitre_technique="Obfuscated Files: Steganography",
        tags=["exfil", "stego"],
    ),
    # Email Exfil
    Signature(
        name="email_exfil",
        pattern=r"(Send-MailMessage.*-Attachments|smtp.*\.Send|Net\.Mail\.SmtpClient)",
        severity="high",
        category=SignatureCategory.EXFILTRATION,
        description="Email-based exfiltration",
        mitre_id="T1048.003",
        mitre_technique="Exfiltration Over Alternative Protocol",
        tags=["exfil", "email"],
    ),
    # FTP Exfil
    Signature(
        name="ftp_exfil",
        pattern=r"(ftp\s+-s:|Net\.WebClient.*ftp://|System\.Net\.FtpWebRequest|curl.*ftp://)",
        severity="medium",
        category=SignatureCategory.EXFILTRATION,
        description="FTP-based exfiltration",
        mitre_id="T1048.003",
        mitre_technique="Exfiltration Over Alternative Protocol",
        tags=["exfil", "ftp"],
    ),
    # Clipboard
    Signature(
        name="clipboard_exfil",
        pattern=r"(Get-Clipboard|Windows\.Clipboard|GetClipboardData|AddClipboardFormatListener)",
        severity="medium",
        category=SignatureCategory.EXFILTRATION,
        description="Clipboard data access",
        mitre_id="T1115",
        mitre_technique="Clipboard Data",
        tags=["exfil", "clipboard"],
    ),
]

# =============================================================================
# RANSOMWARE SIGNATURES (T1486, T1490)
# =============================================================================

RANSOMWARE_SIGNATURES = [
    # Shadow Delete
    Signature(
        name="vss_delete",
        pattern=r"(vssadmin\s+delete\s+shadows|wmic\s+shadowcopy\s+delete|vssadmin\.exe.*\/all|bcdedit.*recoveryenabled.*no)",
        severity="critical",
        category=SignatureCategory.RANSOMWARE,
        description="Volume shadow copy deletion",
        mitre_id="T1490",
        mitre_technique="Inhibit System Recovery",
        tags=["ransomware", "vss"],
    ),
    Signature(
        name="disable_recovery",
        pattern=r"(bcdedit.*recoveryenabled.*no|bcdedit.*bootstatuspolicy.*ignoreallfailures|reagentc.*\/disable)",
        severity="critical",
        category=SignatureCategory.RANSOMWARE,
        description="Windows recovery disabled",
        mitre_id="T1490",
        mitre_technique="Inhibit System Recovery",
        tags=["ransomware", "recovery"],
    ),
    # Mass Encryption
    Signature(
        name="crypto_api_abuse",
        pattern=r"(CryptEncrypt|CryptGenKey|CryptAcquireContext|BCryptEncrypt|AesManaged|RijndaelManaged|System\.Security\.Cryptography)",
        severity="high",
        category=SignatureCategory.RANSOMWARE,
        description="Cryptographic API usage",
        mitre_id="T1486",
        mitre_technique="Data Encrypted for Impact",
        tags=["ransomware", "crypto"],
    ),
    Signature(
        name="file_extension_change",
        pattern=r"(\\.encrypted|\\.locked|\\.crypt|\\.cry|\\.enc|\\.locky|\\.zepto|\\.cerber|\\.wannacry|\\.petya|\\.ryuk)",
        severity="critical",
        category=SignatureCategory.RANSOMWARE,
        description="Ransomware file extension",
        mitre_id="T1486",
        mitre_technique="Data Encrypted for Impact",
        tags=["ransomware", "extension"],
    ),
    # Ransom Note Patterns
    Signature(
        name="ransom_note_content",
        pattern=r"(your\s+files\s+(have\s+been|are)\s+encrypted|pay\s+.*bitcoin|btc\s+wallet|decrypt.*ransom|restore.*files.*payment)",
        severity="critical",
        category=SignatureCategory.RANSOMWARE,
        description="Ransom note text pattern",
        mitre_id="T1486",
        mitre_technique="Data Encrypted for Impact",
        tags=["ransomware", "note"],
    ),
    Signature(
        name="ransom_filename",
        pattern=r"(README.*DECRYPT|HOW.*DECRYPT|DECRYPT.*INSTRUCTION|RANSOM.*NOTE|!!!.*READ.*ME|HELP_DECRYPT)",
        severity="critical",
        category=SignatureCategory.RANSOMWARE,
        description="Ransom note filename pattern",
        mitre_id="T1486",
        mitre_technique="Data Encrypted for Impact",
        tags=["ransomware", "note"],
    ),
    # Mass File Operations
    Signature(
        name="mass_file_enum",
        pattern=r"(Get-ChildItem.*-Recurse.*-Include.*\*\.(doc|xls|pdf|jpg|png)|dir\s+/s\s+/b.*\*\.(doc|xls|pdf)|FindFirstFile.*FindNextFile)",
        severity="medium",
        category=SignatureCategory.RANSOMWARE,
        description="Mass file enumeration",
        mitre_id="T1083",
        mitre_technique="File and Directory Discovery",
        tags=["ransomware", "enum"],
    ),
    # Known Ransomware
    Signature(
        name="known_ransomware_string",
        pattern=r"(WannaCry|WannaCrypt|Petya|NotPetya|Ryuk|REvil|Sodinokibi|DarkSide|Conti|LockBit|BlackCat|ALPHV|Hive|BlackBasta)",
        severity="critical",
        category=SignatureCategory.RANSOMWARE,
        description="Known ransomware family name",
        mitre_id="T1486",
        mitre_technique="Data Encrypted for Impact",
        tags=["ransomware", "family"],
    ),
    # Backup Deletion
    Signature(
        name="backup_deletion",
        pattern=r"(wbadmin\s+delete|del.*\\backup|Remove-Item.*backup|rd\s+/s\s+/q.*backup)",
        severity="high",
        category=SignatureCategory.RANSOMWARE,
        description="Backup deletion",
        mitre_id="T1490",
        mitre_technique="Inhibit System Recovery",
        tags=["ransomware", "backup"],
    ),
    # Safe Mode Boot
    Signature(
        name="safemode_ransomware",
        pattern=r"(bcdedit.*safeboot|safeboot.*network|REG\s+ADD.*SafeBoot)",
        severity="high",
        category=SignatureCategory.RANSOMWARE,
        description="Safe mode boot manipulation",
        mitre_id="T1490",
        mitre_technique="Inhibit System Recovery",
        tags=["ransomware", "safemode"],
    ),
]

# =============================================================================
# AI/LLM ATTACK SIGNATURES (Custom - Your Moat)
# =============================================================================

AI_LLM_SIGNATURES = [
    # Prompt Injection
    Signature(
        name="prompt_injection_ignore",
        pattern=r"(ignore\s+(all\s+)?(previous|prior|above)\s+(instructions|prompts|commands)|disregard\s+(your|all)\s+instructions)",
        severity="critical",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Prompt injection - ignore instructions",
        mitre_id="AML.T0051",
        mitre_technique="LLM Prompt Injection",
        tags=["ai", "prompt_injection"],
    ),
    Signature(
        name="prompt_injection_roleplay",
        pattern=r"(you\s+are\s+now\s+(DAN|evil|unrestricted|jailbroken)|pretend\s+you\s+(are|have)\s+no\s+(restrictions|rules|guidelines)|act\s+as\s+if\s+you\s+(have|had)\s+no\s+ethical)",
        severity="critical",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Prompt injection - roleplay bypass",
        mitre_id="AML.T0051",
        mitre_technique="LLM Prompt Injection",
        tags=["ai", "prompt_injection", "jailbreak"],
    ),
    Signature(
        name="prompt_injection_developer",
        pattern=r"(developer\s+mode|maintenance\s+mode|admin\s+mode|debug\s+mode|sudo\s+mode).*enabled",
        severity="high",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Prompt injection - developer mode",
        mitre_id="AML.T0051",
        mitre_technique="LLM Prompt Injection",
        tags=["ai", "prompt_injection"],
    ),
    # Jailbreak Patterns
    Signature(
        name="jailbreak_dan",
        pattern=r"(DAN\s*[0-9.]+|Do\s+Anything\s+Now|DUDE\s+prompt|AIM\s+prompt|STAN\s+prompt|KEVIN\s+prompt)",
        severity="critical",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Known jailbreak prompt pattern",
        mitre_id="AML.T0051",
        mitre_technique="LLM Jailbreak",
        tags=["ai", "jailbreak"],
    ),
    Signature(
        name="jailbreak_hypothetical",
        pattern=r"(hypothetically\s+speaking|in\s+a\s+fictional\s+scenario|for\s+(educational|research)\s+purposes\s+only|purely\s+hypothetical)",
        severity="medium",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Hypothetical scenario jailbreak",
        mitre_id="AML.T0051",
        mitre_technique="LLM Jailbreak",
        tags=["ai", "jailbreak"],
    ),
    Signature(
        name="jailbreak_base64_encoded",
        pattern=r"(decode\s+this\s+base64|base64\s+decode|aWdub3JlIGFsbCBwcmV2aW91cw==|SWdub3JlIGFsbCBwcmV2aW91cw==)",
        severity="high",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Base64 encoded jailbreak attempt",
        mitre_id="AML.T0051",
        mitre_technique="LLM Jailbreak",
        tags=["ai", "jailbreak", "encoded"],
    ),
    # System Prompt Extraction
    Signature(
        name="system_prompt_leak",
        pattern=r"(repeat\s+(your|the)\s+(system|initial)\s+(prompt|instructions)|what\s+(are|were)\s+your\s+(original|initial)\s+instructions|show\s+me\s+your\s+system\s+prompt)",
        severity="high",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="System prompt extraction attempt",
        mitre_id="AML.T0052",
        mitre_technique="LLM System Prompt Extraction",
        tags=["ai", "prompt_extraction"],
    ),
    Signature(
        name="system_prompt_output",
        pattern=r"(output\s+(your|the)\s+entire\s+(prompt|instructions)|print\s+your\s+configuration|display\s+your\s+rules)",
        severity="high",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="System prompt output request",
        mitre_id="AML.T0052",
        mitre_technique="LLM System Prompt Extraction",
        tags=["ai", "prompt_extraction"],
    ),
    # API Key Leaks
    Signature(
        name="api_key_pattern",
        pattern=r"(sk-[a-zA-Z0-9]{20,}|AIza[0-9A-Za-z_-]{35}|AKIA[0-9A-Z]{16}|ghp_[a-zA-Z0-9]{36}|glpat-[a-zA-Z0-9-]{20})",
        severity="critical",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="API key exposure (OpenAI, Google, AWS, GitHub)",
        mitre_id="T1552.001",
        mitre_technique="Unsecured Credentials: Credentials in Files",
        tags=["ai", "api_key", "credential"],
    ),
    Signature(
        name="api_key_env",
        pattern=r"(OPENAI_API_KEY|ANTHROPIC_API_KEY|GOOGLE_API_KEY|AZURE_OPENAI_KEY|HF_TOKEN|HUGGINGFACE_TOKEN)\s*[=:]\s*['\"]?[\w-]+",
        severity="critical",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="AI service API key in environment/config",
        mitre_id="T1552.001",
        mitre_technique="Unsecured Credentials: Credentials in Files",
        tags=["ai", "api_key", "credential"],
    ),
    # Model Extraction
    Signature(
        name="model_extraction",
        pattern=r"(extract\s+(the|your)\s+model\s+(weights|parameters)|dump\s+your\s+neural\s+network|export\s+model\s+architecture|model\.save|torch\.save)",
        severity="high",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Model extraction attempt",
        mitre_id="AML.T0044",
        mitre_technique="ML Model Extraction",
        tags=["ai", "model_extraction"],
    ),
    Signature(
        name="model_probe",
        pattern=r"(what\s+is\s+your\s+(model|architecture|parameter\s+count)|how\s+many\s+(parameters|layers)|what\s+training\s+data)",
        severity="low",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Model probing questions",
        mitre_id="AML.T0044",
        mitre_technique="ML Model Extraction",
        tags=["ai", "model_probe"],
    ),
    # Data Poisoning
    Signature(
        name="training_data_poison",
        pattern=r"(add\s+this\s+to\s+(your|the)\s+training|remember\s+this\s+for\s+future\s+training|update\s+your\s+(knowledge|training)\s+with)",
        severity="high",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Training data poisoning attempt",
        mitre_id="AML.T0020",
        mitre_technique="ML Training Data Poisoning",
        tags=["ai", "data_poisoning"],
    ),
    Signature(
        name="backdoor_trigger",
        pattern=r"(\[TRIGGER\]|\[\[ADMIN\]\]|\{\{BYPASS\}\}|<OVERRIDE>|#SUDO#|\$\$UNLOCK\$\$)",
        severity="high",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Potential backdoor trigger pattern",
        mitre_id="AML.T0020",
        mitre_technique="ML Backdoor",
        tags=["ai", "backdoor"],
    ),
    # Adversarial Examples
    Signature(
        name="unicode_homoglyph",
        pattern=r"[\u0430\u0435\u043e\u0440\u0441\u0443\u0445\u04bb\u0456\u0458]",  # Cyrillic homoglyphs
        severity="medium",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Unicode homoglyph attack (Cyrillic)",
        mitre_id="AML.T0043",
        mitre_technique="Adversarial ML",
        tags=["ai", "adversarial", "unicode"],
    ),
    Signature(
        name="invisible_chars",
        pattern=r"[\u200b\u200c\u200d\u2060\ufeff]",  # Zero-width chars
        severity="medium",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="Zero-width character injection",
        mitre_id="AML.T0043",
        mitre_technique="Adversarial ML",
        tags=["ai", "adversarial", "invisible"],
    ),
    # Indirect Prompt Injection
    Signature(
        name="indirect_injection",
        pattern=r"(<\|system\|>|<\|assistant\|>|<\|user\|>|\[INST\]|\[/INST\]|<<SYS>>|<</SYS>>)",
        severity="critical",
        category=SignatureCategory.AI_LLM_ATTACKS,
        description="LLM control token injection",
        mitre_id="AML.T0051",
        mitre_technique="LLM Prompt Injection",
        tags=["ai", "prompt_injection", "indirect"],
    ),
]

# =============================================================================
# DETECTOR CLASS
# =============================================================================

def get_all_signatures() -> List[Signature]:
    """Get all defined signatures."""
    return (
        PERSISTENCE_SIGNATURES +
        DEFENSE_EVASION_SIGNATURES +
        CREDENTIAL_ACCESS_SIGNATURES +
        LATERAL_MOVEMENT_SIGNATURES +
        C2_SIGNATURES +
        EXFILTRATION_SIGNATURES +
        RANSOMWARE_SIGNATURES +
        AI_LLM_SIGNATURES
    )


def get_signatures_by_category(category: SignatureCategory) -> List[Signature]:
    """Get signatures for a specific category."""
    return [s for s in get_all_signatures() if s.category == category]


class SignatureDetector:
    """
    Behavioral signature detection engine.
    
    Detects malicious patterns using regex-based signatures
    mapped to MITRE ATT&CK framework.
    """
    
    def __init__(self, signatures: Optional[List[Signature]] = None):
        """Initialize with signatures (defaults to all)."""
        self.signatures = signatures or get_all_signatures()
        self._build_index()
    
    def _build_index(self) -> None:
        """Build category index for fast lookups."""
        self._by_category: Dict[SignatureCategory, List[Signature]] = {}
        for sig in self.signatures:
            if sig.category not in self._by_category:
                self._by_category[sig.category] = []
            self._by_category[sig.category].append(sig)
    
    def detect(self, content: str, file_path: Optional[str] = None) -> List[SignatureMatch]:
        """
        Detect signatures in content.
        
        Args:
            content: Text content to scan
            file_path: Optional file path for context
            
        Returns:
            List of SignatureMatch objects
        """
        matches = []
        lines = content.split('\n')
        
        for sig in self.signatures:
            for match_tuple in sig.match(content):
                start, end, matched_text = match_tuple
                
                # Calculate line number
                line_num = content[:start].count('\n') + 1
                
                # Get context (surrounding lines)
                context_start = max(0, line_num - 2)
                context_end = min(len(lines), line_num + 2)
                context = '\n'.join(lines[context_start:context_end])
                
                matches.append(SignatureMatch(
                    signature=sig,
                    matched_text=matched_text,
                    offset=start,
                    line_number=line_num,
                    context=context,
                ))
        
        return matches
    
    def detect_file(self, file_path: str) -> List[SignatureMatch]:
        """Detect signatures in a file."""
        path = Path(file_path)
        if not path.exists():
            return []
        
        try:
            content = path.read_text(encoding='utf-8', errors='ignore')
            return self.detect(content, str(path))
        except Exception:
            return []
    
    def get_stats(self) -> Dict:
        """Get detector statistics."""
        stats = {
            "total_signatures": len(self.signatures),
            "by_category": {},
            "by_severity": {"low": 0, "medium": 0, "high": 0, "critical": 0},
        }
        
        for cat, sigs in self._by_category.items():
            stats["by_category"][cat.value] = len(sigs)
        
        for sig in self.signatures:
            stats["by_severity"][sig.severity] += 1
        
        return stats
    
    def summarize_matches(self, matches: List[SignatureMatch]) -> Dict:
        """Summarize detection results by category."""
        summary = {
            "total_matches": len(matches),
            "by_category": {},
            "by_severity": {"low": 0, "medium": 0, "high": 0, "critical": 0},
            "mitre_techniques": set(),
        }
        
        for match in matches:
            cat = match.signature.category.value
            if cat not in summary["by_category"]:
                summary["by_category"][cat] = []
            summary["by_category"][cat].append(match.signature.name)
            
            summary["by_severity"][match.signature.severity] += 1
            
            if match.signature.mitre_id:
                summary["mitre_techniques"].add(match.signature.mitre_id)
        
        summary["mitre_techniques"] = list(summary["mitre_techniques"])
        return summary


# Quick test
if __name__ == "__main__":
    detector = SignatureDetector()
    stats = detector.get_stats()
    
    print("Signature Detector Statistics")
    print("=" * 40)
    print(f"Total signatures: {stats['total_signatures']}")
    print("\nBy category:")
    for cat, count in sorted(stats["by_category"].items()):
        print(f"  {cat}: {count}")
    print("\nBy severity:")
    for sev, count in stats["by_severity"].items():
        print(f"  {sev}: {count}")
