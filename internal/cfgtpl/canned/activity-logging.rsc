# Activity analytics: DNSUSER and USB logging (RouterOS 7.24.5 reference).
# BEFORE DEPLOYING
# 1. Attach USB storage to the router. In System > Disks / Files confirm its
#    mounted name; usb1-part1 is an example, not a guaranteed disk name.
# 2. Create Log on that mounted disk in Files. The directory MUST exist first.
#    This template does not format, mount, erase or create directories.
# 3. Set archive_path to that directory plus /netlog (no .0.txt extension).
#    Use the DIRECTORY only in Activity > Synchronization, e.g. usb1-part1/Log.
# 4. Clients must already use this router for DNS, with LAN DNS allowed and
#    WAN DNS protected by its firewall. No DNS/firewall/DHCP settings change here.
# 5. Preview first: DNSUserLine's source and its read,write,test policy are set.
#    dont-require-permissions=yes permits the logging hook to call the script.
#    Existing manual MAC labels in that script are replaced by empty maps.
#    A working existing setup does not need this template re-applied.
# 6. After deploying, query DNS from a LAN client, verify DNSUSER in Logs and
#    netlog.0.txt in Files, then enable Activity > Synchronization for this
#    router with the same folder and router log timezone. Check the sync status.
# MikroDash needs actual /file/read access; verify it, not just group policies.
# Script hooks skip messages while busy. This script records IPv4 DNS clients
# and cached A/AAAA answers; a cold cache may have no answer. Resolve fallback
# stays OFF. DNS events are not confirmed visits, browsing time or bandwidth.
# USB logs ROTATE: file_count x lines_per_file is a line limit, not 30 days.
# Overwritten files cannot be recovered. MikroDash retains imported data 30 days.
# HS_ records are archived for existing HotSpot hooks; Activity does not count
# them as DNS events. Firewall logging is optional and is not enabled here.
# Reuse named actions and exact rules; never delete existing archive files or
# unrelated logging rules. There is no scheduler: DNSJoin supplies $message.
/system script
ensure name=DNSUserLine
set [ find name=DNSUserLine ] policy=read,write,test dont-require-permissions=yes comment="mdcfg:activity-logging" source="# DNSUserLine - working RouterOS 7.24.5 logging-action script\n\
    # Usage: Script action DNSJoin; incoming \$message is a DNS query log entry.\n\
    # Output: DNSUSER {Connection:{...},Destination:{...},Source:{...}}\n\
    # Data sources: HotSpot Active -> DHCP bound lease -> complete ARP entry.\n\
    # No RouterOS network configuration changes; only writes DNSUSER log entries.\n\
    \n\
    # Leave false for normal operation to avoid per-query blocking DNS lookups.\n\
    # If true, test with one client only; synchronous lookups may lose log events.\n\
    :local allowResolveFallback false\n\
    \n\
    # OPTIONAL manual user names by device MAC (use uppercase MACs).\n\
    # Empty by default. Add only your own mappings on the router if needed.\n\
    :local manualUsersByMAC [:toarray \"\"]\n\
    \n\
    # OPTIONAL manual device labels (override DHCP host-name for matching MACs).\n\
    :local manualDevicesByMAC [:toarray \"\"]\n\
    \n\
    :local raw [:tostr \$message]\n\
    \n\
    :if ([:len \$raw] = 0) do={\n\
        :log warning \"DNSUSER_ERROR stage=input detail=empty message; use DNSJoin\"\n\
    } else={\n\
        :local stage \"parse-dns\"\n\
        :onerror err in={\n\
            # -------- Read query source IP, domain, query type --------\n\
            :local p [:find \$raw \"query from \"]\n\
            :if ([:typeof \$p] != \"num\") do={ :error \"Missing query from\" }\n\
            :local ipStart (\$p + 11)\n\
            :local ipEnd [:find \$raw \": #\" \$ipStart]\n\
            :if ([:typeof \$ipEnd] != \"num\") do={ :error \"Missing client IP delimiter\" }\n\
            :local clientIP [:toip [:pick \$raw \$ipStart \$ipEnd]]\n\
            :if ([:typeof \$clientIP] != \"ip\") do={ :error \"Invalid IPv4 client\" }\n\
    \n\
            :local idEnd [:find \$raw \" \" (\$ipEnd + 3)]\n\
            :if ([:typeof \$idEnd] != \"num\") do={ :error \"Missing DNS query ID\" }\n\
            :local nameStart (\$idEnd + 1)\n\
            :local nameEnd [:find \$raw \" \" \$nameStart]\n\
            :if ([:typeof \$nameEnd] != \"num\") do={ :error \"Missing DNS query type\" }\n\
            :local dnsName [:pick \$raw \$nameStart \$nameEnd]\n\
            :local queryType [:pick \$raw (\$nameEnd + 1) [:len \$raw]]\n\
            :if (([:len \$dnsName] = 0) || ([:len \$queryType] = 0)) do={ :error \"Empty DNS domain or type\" }\n\
            :if ([:len \$dnsName] > 1) do={\n\
                :if ([:pick \$dnsName ([:len \$dnsName] - 1)] = \".\") do={\n\
                    :set dnsName [:pick \$dnsName 0 ([:len \$dnsName] - 1)]\n\
                }\n\
            }\n\
            :if (\$queryType = \"UNKNOWN (65)\") do={ :set queryType \"HTTPS\" }\n\
    \n\
            # -------- HotSpot session: highest identity priority --------\n\
            :set stage \"hotspot\"\n\
            :local account \"-\"\n\
            :local clientMAC \"-\"\n\
            :local hsServer \"-\"\n\
            :local deviceName \"-\"\n\
            :local matchState \"unknown\"\n\
    \n\
            :local hsIDs [/ip hotspot active find where address=\$clientIP]\n\
            :if ([:len \$hsIDs] = 1) do={\n\
                :local hsID [:pick \$hsIDs 0]\n\
                :set account [:tostr [/ip hotspot active get \$hsID user]]\n\
                :set clientMAC [:tostr [/ip hotspot active get \$hsID mac-address]]\n\
                :set hsServer [:tostr [/ip hotspot active get \$hsID server]]\n\
                :set matchState \"active-now\"\n\
            }\n\
            :if ([:len \$hsIDs] > 1) do={ :set matchState \"ambiguous\" }\n\
    \n\
            # -------- DHCP bound lease: hostname and MAC --------\n\
            :set stage \"dhcp\"\n\
            :local leaseIDs [/ip dhcp-server lease find where address=\$clientIP and status=\"bound\"]\n\
            :if ([:len \$leaseIDs] = 1) do={\n\
                :local leaseID [:pick \$leaseIDs 0]\n\
                :local dhcpMAC [:tostr [/ip dhcp-server lease get \$leaseID mac-address]]\n\
                :local dhcpHost [:tostr [/ip dhcp-server lease get \$leaseID host-name]]\n\
                :if (([:len \$dhcpMAC] > 0) && (\$dhcpMAC != \"00:00:00:00:00:00\")) do={\n\
                    :local normalizedDHCPMAC [:convert \$dhcpMAC transform=uc]\n\
                    :local normalizedCurrentMAC [:convert \$clientMAC transform=uc]\n\
                    :if ((\$clientMAC = \"-\") || (\$clientMAC = \"\") || (\$normalizedDHCPMAC = \$normalizedCurrentMAC)) do={\n\
                        :if ((\$clientMAC = \"-\") || (\$clientMAC = \"\")) do={\n\
                            :set clientMAC \$normalizedDHCPMAC\n\
                        }\n\
                        :if ([:len \$dhcpHost] > 0) do={ :set deviceName \$dhcpHost }\n\
                        :if (\$matchState = \"unknown\") do={ :set matchState \"dhcp-only\" }\n\
                    }\n\
                }\n\
            }\n\
    \n\
            # -------- ARP fallback: valid MAC, no asserted user name --------\n\
            :if ((\$clientMAC = \"-\") || (\$clientMAC = \"\")) do={\n\
                :set stage \"arp\"\n\
                :local arpIDs [/ip arp find where address=\$clientIP and complete=yes]\n\
                :if ([:len \$arpIDs] = 1) do={\n\
                    :local arpID [:pick \$arpIDs 0]\n\
                    :local arpMAC [:tostr [/ip arp get \$arpID mac-address]]\n\
                    :if (([:len \$arpMAC] > 0) && (\$arpMAC != \"00:00:00:00:00:00\")) do={\n\
                        :set clientMAC [:convert \$arpMAC transform=uc]\n\
                        :if (\$matchState = \"unknown\") do={ :set matchState \"arp-only\" }\n\
                    }\n\
                }\n\
            }\n\
    \n\
            # -------- Optional operator-maintained MAC labels --------\n\
            :set stage \"manual-labels\"\n\
            :if (([:len \$clientMAC] > 0) && (\$clientMAC != \"-\")) do={\n\
                :local macKey [:convert \$clientMAC transform=uc]\n\
                :local manualDevice (\$manualDevicesByMAC->\$macKey)\n\
                :if ([:typeof \$manualDevice] = \"str\") do={\n\
                    :if ([:len \$manualDevice] > 0) do={ :set deviceName \$manualDevice }\n\
                }\n\
                # Never replace a real HotSpot login with a manually assigned user.\n\
                :if ((\$matchState != \"active-now\") && (\$matchState != \"ambiguous\")) do={\n\
                    :local manualUser (\$manualUsersByMAC->\$macKey)\n\
                    :if ([:typeof \$manualUser] = \"str\") do={\n\
                        :if ([:len \$manualUser] > 0) do={\n\
                            :set account \$manualUser\n\
                            :set matchState \"manual-mac\"\n\
                        }\n\
                    }\n\
                }\n\
            }\n\
    \n\
            # -------- DNS A/AAAA from cache; follow CNAME up to 5 hops --------\n\
            :set stage \"dns-cache\"\n\
            :local dnsIPs [:toarray \"\"]\n\
            :local ipBasis \"unavailable\"\n\
            :local canonicalName \"-\"\n\
            :local searchName \$dnsName\n\
            :local hop 0\n\
            :local finished false\n\
            :while ((\$hop < 5) && (\$finished = false)) do={\n\
                :foreach r in=[/ip dns cache all find where name=\$searchName and type=\"A\"] do={\n\
                    :if ([:len \$dnsIPs] < 4) do={\n\
                        :local v [:tostr [/ip dns cache all get \$r data]]\n\
                        :if ([:len \$v] > 0) do={ :set dnsIPs (\$dnsIPs, \$v) }\n\
                    }\n\
                }\n\
                :foreach r in=[/ip dns cache all find where name=\$searchName and type=\"AAAA\"] do={\n\
                    :if ([:len \$dnsIPs] < 8) do={\n\
                        :local v [:tostr [/ip dns cache all get \$r data]]\n\
                        :if ([:len \$v] > 0) do={ :set dnsIPs (\$dnsIPs, \$v) }\n\
                    }\n\
                }\n\
                :if ([:len \$dnsIPs] > 0) do={\n\
                    :if (\$hop = 0) do={ :set ipBasis \"dns-cache\" } else={ :set ipBasis \"dns-cache-cname\" }\n\
                    :set finished true\n\
                } else={\n\
                    :local cnames [/ip dns cache all find where name=\$searchName and type=\"CNAME\"]\n\
                    :if ([:len \$cnames] > 0) do={\n\
                        :local nextName [:tostr [/ip dns cache all get [:pick \$cnames 0] data]]\n\
                        :if ([:len \$nextName] > 1) do={\n\
                            :if ([:pick \$nextName ([:len \$nextName] - 1)] = \".\") do={\n\
                                :set nextName [:pick \$nextName 0 ([:len \$nextName] - 1)]\n\
                            }\n\
                        }\n\
                        :if ((\$nextName = \"\") || (\$nextName = \$searchName)) do={\n\
                            :set finished true\n\
                        } else={\n\
                            :set searchName \$nextName\n\
                            :set canonicalName \$nextName\n\
                        }\n\
                    } else={ :set finished true }\n\
                }\n\
                :set hop (\$hop + 1)\n\
            }\n\
    \n\
            # Off by default: DNS queries run synchronously and increase log loss.\n\
            :if ((\$allowResolveFallback = true) && ([:len \$dnsIPs] = 0) && (\$queryType = \"A\")) do={\n\
                :set stage \"resolve-fallback\"\n\
                :local resolvedIP \"\"\n\
                :onerror lookupErr in={\n\
                    :set resolvedIP [:tostr [:resolve domain-name=\$dnsName type=ipv4]]\n\
                } do={ :set resolvedIP \"\" }\n\
                :if ([:len \$resolvedIP] > 0) do={\n\
                    :set dnsIPs (\$dnsIPs, \$resolvedIP)\n\
                    :set ipBasis \"dns-resolve\"\n\
                }\n\
            }\n\
    \n\
            # -------- Normalize fields before serialization --------\n\
            :set stage \"json\"\n\
            :if ([:len \$account] = 0) do={ :set account \"-\" }\n\
            :if ([:len \$clientMAC] = 0) do={ :set clientMAC \"-\" }\n\
            :if ([:len \$hsServer] = 0) do={ :set hsServer \"-\" }\n\
            :if ([:len \$deviceName] = 0) do={ :set deviceName \"-\" }\n\
    \n\
            :local jServer [:serialize to=json options=json.no-string-conversion value=[:tostr \$hsServer]]\n\
            :local jType [:serialize to=json options=json.no-string-conversion value=[:tostr \$queryType]]\n\
            :local jMatch [:serialize to=json options=json.no-string-conversion value=[:tostr \$matchState]]\n\
            :local jDomain [:serialize to=json options=json.no-string-conversion value=[:tostr \$dnsName]]\n\
            :local jCanonical [:serialize to=json options=json.no-string-conversion value=[:tostr \$canonicalName]]\n\
            :local jIPs [:serialize to=json options=json.no-string-conversion value=\$dnsIPs]\n\
            :local jBasis [:serialize to=json options=json.no-string-conversion value=[:tostr \$ipBasis]]\n\
            :local jUser [:serialize to=json options=json.no-string-conversion value=[:tostr \$account]]\n\
            :local jIP [:serialize to=json options=json.no-string-conversion value=[:tostr \$clientIP]]\n\
            :local jMAC [:serialize to=json options=json.no-string-conversion value=[:tostr \$clientMAC]]\n\
            :local jDevice [:serialize to=json options=json.no-string-conversion value=[:tostr \$deviceName]]\n\
    \n\
            # Explicit JSON serialization preserves strings, arrays and the field names.\n\
            :local payload (\"{\\\"Connection\\\":{\\\"server\\\":\" . \$jServer)\n\
            :set payload (\$payload . \",\\\"qtype\\\":\" . \$jType)\n\
            :set payload (\$payload . \",\\\"match\\\":\" . \$jMatch)\n\
            :set payload (\$payload . \"},\\\"Destination\\\":{\\\"domain\\\":\" . \$jDomain)\n\
            :set payload (\$payload . \",\\\"canonical\\\":\" . \$jCanonical)\n\
            :set payload (\$payload . \",\\\"dns_ips\\\":\" . \$jIPs)\n\
            :set payload (\$payload . \",\\\"ip_basis\\\":\" . \$jBasis)\n\
            :set payload (\$payload . \"},\\\"Source\\\":{\\\"user\\\":\" . \$jUser)\n\
            :set payload (\$payload . \",\\\"ip\\\":\" . \$jIP)\n\
            :set payload (\$payload . \",\\\"mac\\\":\" . \$jMAC)\n\
            :set payload (\$payload . \",\\\"device_name\\\":\" . \$jDevice)\n\
            :set payload (\$payload . \"}}\")\n\
            :set stage \"write-log\"\n\
            :log info (\"DNSUSER \" . \$payload)\n\
        } do={\n\
            :log warning (\"DNSUSER_ERROR stage=\" . \$stage . \" detail=\" . [:tostr \$err])\n\
        }\n\
    }"
/system logging action
ensure name=NetArchive
set [ find name=NetArchive ] target=disk disk-file-name={{archive_path}} disk-lines-per-file={{lines_per_file}} disk-file-count={{file_count}} disk-stop-on-full=no
ensure name=DNSJoin
set [ find name=DNSJoin ] target=script script=DNSUserLine
/system logging
ensure action=NetArchive topics=script regex="^DNSUSER"
set [ find action=NetArchive topics=script regex="^DNSUSER" ] disabled=no
ensure action=NetArchive topics=script,info regex="^HS_"
set [ find action=NetArchive topics=script,info regex="^HS_" ] disabled=no
ensure action=DNSJoin topics=dns regex="query from [0-9.]+: #"
set [ find action=DNSJoin topics=dns regex="query from [0-9.]+: #" ] disabled=no
