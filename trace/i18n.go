package trace

import (
	"fmt"
	"os"
	"strings"
)

// The traces speak English or French (--lang, else QUELLOG_LANG, else LANG).
// The rest of the quellog report stays in English.

var lang = "en"

// SetLang chooses the language of the traces: "en" or "fr". An empty value
// keeps the default, read from QUELLOG_LANG then LANG.
func SetLang(l string) error {
	switch l = strings.ToLower(strings.TrimSpace(l)); l {
	case "":
		lang = DefaultLang()
	case "en", "fr":
		lang = l
	default:
		return fmt.Errorf("unsupported language %q (en or fr)", l)
	}
	return nil
}

// Lang returns the language of the traces.
func Lang() string { return lang }

// DefaultLang reads QUELLOG_LANG, then LANG ("fr_FR.UTF-8" → fr).
func DefaultLang() string {
	for _, v := range []string{os.Getenv("QUELLOG_LANG"), os.Getenv("LANG")} {
		if v = strings.ToLower(v); v == "" {
			continue
		}
		if strings.HasPrefix(v, "fr") {
			return "fr"
		}
		return "en"
	}
	return "en"
}

// T formats a message of the catalog in the current language.
func T(key string, args ...any) string {
	m, ok := messages[key]
	if !ok {
		return key
	}
	format := m[0]
	if lang == "fr" && m[1] != "" {
		format = m[1]
	}
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// decimal writes a number with the decimal separator of the language.
func decimal(format string, v float64) string {
	s := fmt.Sprintf(format, v)
	if lang == "fr" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// HumanDuration formats milliseconds as "850 ms", "2.1 s", "7m 38s".
func HumanDuration(ms float64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%.0f ms", ms)
	case ms < 60000:
		return decimal("%.1f", ms/1000) + " s"
	case ms < 3600000:
		s := int(ms / 1000)
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	}
	m := int(ms / 60000)
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

// HumanBytes formats a size: "919.2 MB" (en), "919,2 Mo" (fr).
func HumanBytes(b int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB", "PB"}
	if lang == "fr" {
		units = []string{"o", "Ko", "Mo", "Go", "To", "Po"}
	}
	if b < 1024 {
		return fmt.Sprintf("%d %s", b, units[0])
	}
	v, i := float64(b), 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return decimal("%.1f", v) + " " + units[i]
}

// HumanCount formats a row count: "1.4 M", "121".
func HumanCount(n float64) string {
	switch {
	case n >= 1e9:
		return decimal("%.1f", n/1e9) + " G"
	case n >= 1e6:
		return decimal("%.1f", n/1e6) + " M"
	case n >= 1e4:
		return fmt.Sprintf("%.0f k", n/1e3)
	}
	return fmt.Sprintf("%.0f", n)
}

// Percent formats a share: "0.3 %", "96 %".
func Percent(share float64) string {
	p := 100 * share
	if p < 10 {
		return decimal("%.1f", p) + " %"
	}
	return fmt.Sprintf("%.0f %%", p)
}

// messages holds each text in English and French.
var messages = map[string][2]string{
	// Time split
	"split.unknown": {
		"No auto_explain plan was logged for this statement (faster than auto_explain.log_min_duration, or auto_explain not loaded): only the total is known.",
		"Aucun plan auto_explain n'a été journalisé pour cette requête (plus rapide que auto_explain.log_min_duration, ou auto_explain absent) : seule la durée totale est connue.",
	},
	"split.client": {
		"An SQL EXECUTE stores its whole result before sending it. The database finished its work in %s; the rest (%s) was spent sending rows to the client (wait event ClientWrite): a slow network or a client reading slowly.",
		"Un EXECUTE en SQL stocke tout son résultat avant de l'envoyer. La base a fini son travail en %s ; le reste (%s) a servi à envoyer les lignes au client (événement d'attente ClientWrite) : réseau lent ou client qui lit lentement.",
	},
	"split.client.stored": {
		" The stored result was %s (temp file freed at the end).",
		" Le résultat stocké faisait %s (fichier temporaire libéré à la fin).",
	},
	"split.outside": {
		"%s of the time was spent outside the executor: parsing and planning, lock waits before the execution, triggers or commit. Rows sent to the client are already counted in the executor time for a plain statement.",
		"%s du temps s'est passé hors de l'exécuteur : analyse et planification, attente de verrous avant l'exécution, triggers ou commit. Pour une requête simple, l'envoi des lignes au client est déjà compté dans le temps de l'exécuteur.",
	},
	"split.executor": {
		"Nearly all the time was spent in the executor (for a plain statement this includes sending rows to the client).",
		"Presque tout le temps s'est passé dans l'exécuteur (pour une requête simple, l'envoi des lignes au client y est compris).",
	},

	// Impact, shared by the findings that point at one plan node
	"impact.major": {
		"This step took %s, %s of the statement's time: it matters.",
		"Cette étape a pris %s, %s du temps de la requête : elle compte.",
	},
	"impact.minor": {
		"This step took %s, %s of the statement's time: not the cause here.",
		"Cette étape a pris %s, %s du temps de la requête : ce n'est pas la cause ici.",
	},
	"impact.unknown": {
		"The plan does not tell how long this step took (no actual times: auto_explain.log_analyze is off).",
		"Le plan ne dit pas combien de temps a pris cette étape (pas de temps réels : auto_explain.log_analyze est désactivé).",
	},

	// Sending the result to the client
	"client.title": {
		"Sending the result took %s (%s of the time)",
		"L'envoi du résultat a pris %s (%s du temps)",
	},
	"client.why": {
		"%s were sent in %s: %s/s (about %s Mbit/s). The database itself had finished in %s.",
		"%s ont été envoyés en %s, soit %s/s (environ %s Mbit/s). La base, elle, avait fini en %s.",
	},
	"client.why.rows": {
		" The result is %s rows of %d bytes on average.",
		" Le résultat compte %s lignes de %d octets en moyenne.",
	},
	"client.impact": {
		"This is where the time went: the bottleneck is the network or the client, not the database.",
		"C'est là que le temps est passé : le goulot est le réseau ou le client, pas la base.",
	},
	"client.check": {
		"-- while it runs: ClientWrite means the server is waiting for the client\nSELECT pid, client_addr, state, wait_event_type, wait_event, now() - query_start AS running\nFROM pg_stat_activity WHERE wait_event = 'ClientWrite';",
		"-- pendant l'exécution : ClientWrite veut dire que le serveur attend le client\nSELECT pid, client_addr, state, wait_event_type, wait_event, now() - query_start AS duree\nFROM pg_stat_activity WHERE wait_event = 'ClientWrite';",
	},
	"client.proposal": {
		"Check the network path to %s (throughput, latency) and how the application reads the result. Fewer rows or fewer columns (%d bytes per row) shorten the transfer.",
		"Vérifier le chemin réseau vers %s (débit, latence) et la façon dont l'application lit le résultat. Moins de lignes ou moins de colonnes (%d octets par ligne) raccourcissent le transfert.",
	},
	"client.proposal.qt": {
		" The statement name (qpsqlpstmt_…) is the Qt QPSQL driver: it sends PREPARE/EXECUTE as SQL, so the server stores the whole result before sending it; on the client side, QSqlQuery::setForwardOnly(true) avoids also caching every row.",
		" Le nom de la requête (qpsqlpstmt_…) est celui du driver Qt QPSQL : il envoie PREPARE/EXECUTE en SQL, si bien que le serveur stocke tout le résultat avant de l'envoyer ; côté client, QSqlQuery::setForwardOnly(true) évite en plus de garder toutes les lignes en mémoire.",
	},
	"client.unknown": {"the client", "le client"},

	// Large result, when sending is part of the executor time
	"result.title": {
		"Large result: %s rows × %d B ≈ %s sent to the client",
		"Gros résultat : %s lignes × %d o ≈ %s envoyés au client",
	},
	"result.why": {
		"The statement returns %s rows; for a plain statement, sending them is part of the executor time.",
		"La requête renvoie %s lignes ; pour une requête simple, leur envoi fait partie du temps de l'exécuteur.",
	},
	"result.proposal": {
		"Check with the developers that the application needs every row: a missing filter, no LIMIT or no paging make the database send everything.",
		"Vérifier avec les développeurs que l'application a besoin de toutes ces lignes : un filtre manquant, pas de LIMIT ou pas de pagination font tout envoyer.",
	},

	// Sort spilled to disk
	"sort.title": {
		"%s spilled %s to disk",
		"%s a débordé de %s sur disque",
	},
	"sort.why": {
		"external merge: the sort did not fit in work_mem; %s written by %d process(es), the largest %s.",
		"tri externe (external merge) : le tri ne tenait pas dans work_mem ; %s écrits par %d processus, le plus gros %s.",
	},
	"sort.check": {
		"SHOW work_mem;\nSELECT name, setting, unit, source FROM pg_settings\nWHERE name IN ('work_mem', 'hash_mem_multiplier', 'temp_file_limit', 'log_temp_files');",
		"",
	},
	"sort.proposal": {
		"work_mem applies to each sort of each process. To keep this one in memory, give it more for this role or session only (for example ALTER ROLE … SET work_mem = '%s'), not globally.",
		"work_mem vaut pour chaque tri de chaque processus. Pour garder celui-ci en mémoire, l'augmenter pour ce rôle ou cette session seulement (par exemple ALTER ROLE … SET work_mem = '%s'), pas pour toute l'instance.",
	},
	"sort.proposal.index": {
		" An index on %s (%s) could also return the rows already sorted.",
		" Un index sur %s (%s) pourrait aussi rendre les lignes déjà triées.",
	},

	// Hash split into batches
	"hash.title": {
		"%s: hash split into %s batches",
		"%s : hachage découpé en %s lots",
	},
	"hash.why": {
		"The hash table did not fit in work_mem × hash_mem_multiplier and went to disk.",
		"La table de hachage ne tenait pas dans work_mem × hash_mem_multiplier et est passée sur disque.",
	},
	"hash.proposal": {
		"Raise hash_mem_multiplier or work_mem for this role or session only.",
		"Augmenter hash_mem_multiplier ou work_mem pour ce rôle ou cette session seulement.",
	},

	// Scan that throws most rows away
	"filter.title": {
		"%s reads %s rows to keep %s",
		"%s lit %s lignes pour en garder %s",
	},
	"filter.why": {
		"The filter keeps %s of the rows it reads: an index on the filtered columns would read only those.",
		"Le filtre garde %s des lignes lues : un index sur les colonnes filtrées ne lirait que celles-là.",
	},
	"filter.why.cast": {
		" The filter casts %s to %s: if the column is not of that type, an index on it is not used.",
		" Le filtre convertit %s en %s : si la colonne n'est pas de ce type, un index dessus n'est pas utilisé.",
	},
	"filter.check": {
		"SELECT indexname, indexdef FROM pg_indexes WHERE tablename = '%[1]s';\nSELECT attname, n_distinct, null_frac, most_common_vals FROM pg_stats\nWHERE tablename = '%[1]s' AND attname IN (%[2]s);\nSELECT column_name, data_type FROM information_schema.columns\nWHERE table_name = '%[1]s' AND column_name IN (%[2]s);",
		"",
	},
	"filter.proposal": {
		"To propose to the DBA: CREATE INDEX CONCURRENTLY ON %s (%s); (CONCURRENTLY does not block writes; check first that no index already covers these columns).",
		"À proposer au DBA : CREATE INDEX CONCURRENTLY ON %s (%s); (CONCURRENTLY ne bloque pas les écritures ; vérifier d'abord qu'aucun index ne couvre déjà ces colonnes).",
	},

	// Planner estimate far from reality
	"estimate.title": {
		"%s: estimated %s rows, got %s",
		"%s : %s lignes estimées, %s obtenues",
	},
	"estimate.why": {
		"The planner's estimate is off by %s×: the plan built on it (join order, method, memory) may be the wrong one.",
		"L'estimation du planificateur se trompe d'un facteur %s : le plan bâti dessus (ordre des jointures, méthode, mémoire) peut être le mauvais.",
	},
	"estimate.check": {
		"SELECT relname, n_live_tup, n_mod_since_analyze, last_analyze, last_autoanalyze\nFROM pg_stat_user_tables WHERE relname = '%s';",
		"",
	},
	"estimate.proposal": {
		"Statistics may be stale: ANALYZE %s (by the DBA). When several conditions are correlated, CREATE STATISTICS on these columns helps the planner.",
		"Les statistiques sont peut-être périmées : ANALYZE %s (par le DBA). Si plusieurs conditions sont liées, un CREATE STATISTICS sur ces colonnes aide le planificateur.",
	},

	// Parallel workers missing
	"workers.title": {
		"%s: %s workers planned, %s launched",
		"%s : %s workers prévus, %s lancés",
	},
	"workers.why": {
		"Not enough parallel workers were free at that moment: the step ran with less parallelism than planned.",
		"Il n'y avait pas assez de workers parallèles libres à ce moment : l'étape a tourné moins en parallèle que prévu.",
	},
	"workers.check": {
		"SHOW max_parallel_workers;\nSHOW max_worker_processes;\nSELECT count(*) FROM pg_stat_activity WHERE backend_type = 'parallel worker';",
		"",
	},
	"workers.proposal": {
		"Other parallel queries were using the workers. If the CPUs allow it, the DBA can raise max_parallel_workers (and max_worker_processes).",
		"D'autres requêtes parallèles occupaient les workers. Si les CPU le permettent, le DBA peut augmenter max_parallel_workers (et max_worker_processes).",
	},

	// Condition repeated in a filter
	"dup.title": {
		"%s: the same condition appears twice",
		"%s : la même condition apparaît deux fois",
	},
	"dup.why": {
		"%s is repeated in the filter.",
		"%s est répétée dans le filtre.",
	},
	"dup.impact": {
		"Harmless for the result and for the time.",
		"Sans effet sur le résultat ni sur le temps.",
	},
	"dup.proposal": {
		"To report to the developers: the application builds its WHERE clause by repeating a condition.",
		"À signaler aux développeurs : l'application construit sa clause WHERE en répétant une condition.",
	},

	// JIT compilation
	"jit.title": {
		"JIT compilation took %s",
		"La compilation JIT a pris %s",
	},
	"jit.why": {
		"Of %s in the executor, %s went into compiling expressions (JIT).",
		"Sur %s dans l'exécuteur, %s sont partis dans la compilation des expressions (JIT).",
	},
	"jit.check": {
		"SHOW jit;\nSHOW jit_above_cost;",
		"",
	},
	"jit.proposal": {
		"For this kind of query, jit = off (for the role or the session) or a higher jit_above_cost avoids the cost.",
		"Pour ce genre de requête, jit = off (pour le rôle ou la session) ou un jit_above_cost plus haut évite ce coût.",
	},

	// Lock wait
	"lock.title": {
		"Waited %s for a lock",
		"Attente de %s sur un verrou",
	},
	"lock.why": {
		"The backend logged: %s",
		"Le backend a journalisé : %s",
	},
	"lock.check": {
		"-- while it waits: who blocks whom\nSELECT pid, wait_event_type, wait_event, pg_blocking_pids(pid) AS blocked_by, query\nFROM pg_stat_activity WHERE wait_event_type = 'Lock';",
		"-- pendant l'attente : qui bloque qui\nSELECT pid, wait_event_type, wait_event, pg_blocking_pids(pid) AS bloque_par, query\nFROM pg_stat_activity WHERE wait_event_type = 'Lock';",
	},
	"lock.proposal": {
		"The timeline shows the lock lines and the process holding the lock: that transaction is the one to look at.",
		"La chronologie montre les lignes de verrou et le processus qui tenait le verrou : c'est cette transaction qu'il faut regarder.",
	},

	// Labels of the renderings
	"label.sep":          {": ", " : "},
	"label.why":          {"Why", "Pourquoi"},
	"label.impact":       {"Impact here", "Impact ici"},
	"label.check":        {"To check (read-only)", "À vérifier (lecture seule)"},
	"label.proposal":     {"To propose", "À proposer"},
	"label.trace":        {"TRACE", "TRACE"},
	"label.session":      {"Session", "Session"},
	"label.statement":    {"Statement", "Instruction"},
	"label.window":       {"Window", "Fenêtre"},
	"label.workers":      {"Workers", "Workers"},
	"label.workers.note": {"parallel workers seen through their temp files", "workers parallèles repérés par leurs fichiers temporaires"},
	"label.time":         {"WHERE THE TIME WENT", "OÙ EST PASSÉ LE TEMPS"},
	"label.executor":     {"Executor (auto_explain)", "Exécuteur (auto_explain)"},
	"label.after":        {"After the executor", "Après l'exécuteur"},
	"label.query":        {"QUERY", "REQUÊTE"},
	"label.parameters":   {"Parameters", "Paramètres"},
	"label.prepared":     {"Prepared text found in DETAIL: prepare (no need to search for the PREPARE).", "Texte préparé trouvé dans DETAIL: prepare (inutile de chercher le PREPARE)."},
	"label.replay":       {"REPLAY", "REJOUER"},
	"label.replay.note":  {"EXPLAIN without ANALYZE only plans the query. EXPLAIN (ANALYZE, BUFFERS) runs it for real: same cost as in production.", "EXPLAIN sans ANALYZE ne fait que planifier la requête. EXPLAIN (ANALYZE, BUFFERS) l'exécute vraiment : même coût qu'en production."},
	"label.plan":         {"PLAN  (auto_explain at %s, %s in the executor)", "PLAN  (auto_explain à %s, %s dans l'exécuteur)"},
	"label.plan.rebuilt": {"The log had flattened this plan onto one line: its indentation is rebuilt.", "Le journal avait mis ce plan sur une seule ligne : son indentation est reconstituée."},
	"label.findings":     {"FINDINGS", "CONSTATS"},
	"label.foreign":      {"OTHER PLANS ON THIS BACKEND  (not this statement)", "AUTRES PLANS SUR CE BACKEND  (pas cette requête)"},
	"label.foreign.line": {"%s  %s in the executor  Query Text: %s", "%s  %s dans l'exécuteur  Query Text : %s"},
	"label.foreign.note": {"These plans were logged by the same backend during the window, but their Query Text is another statement: they are not this statement's plan.", "Ces plans ont été journalisés par le même backend pendant la fenêtre, mais leur Query Text est une autre requête : ce ne sont pas les plans de celle-ci."},
	"label.temp":         {"TEMP FILES  (%d files, %s)", "FICHIERS TEMPORAIRES  (%d fichiers, %s)"},
	"label.temp.stored":  {"← stored result, freed after sending", "← résultat stocké, libéré après l'envoi"},
	"label.timeline":     {"TIMELINE", "CHRONOLOGIE"},
	"label.traced":       {"SLOWEST STATEMENTS TRACED", "REQUÊTES LES PLUS LENTES, TRACÉES"},
	"label.ended":        {"ended %s", "finie à %s"},
	"label.split.short":  {"executor %s, after the executor %s", "exécuteur %s, après l'exécuteur %s"},
	"label.temp.short":   {"temp files: %d, %s", "fichiers temporaires : %d, %s"},
	"label.full":         {"full trace: quellog trace <logs> --pid %s", "trace complète : quellog trace <journaux> --pid %s"},
	"label.md.section":   {"Slowest statements traced", "Requêtes les plus lentes, tracées"},
	"label.md.parallel":  {"Parallel workers", "Workers parallèles"},
	"label.md.other":     {"Other plan on this backend, not this statement: %s, %s, Query Text `%s`", "Autre plan sur ce backend, pas cette requête : %s, %s, Query Text `%s`"},
	"label.md.temp.head": {"| Time | PID | Role | Size | Path |", "| Heure | PID | Rôle | Taille | Chemin |"},
	"label.md.time.head": {"| Time | PID | Kind | Event |", "| Heure | PID | Type | Événement |"},
	"label.stored":       {"stored result", "résultat stocké"},
	"label.none":         {"No timed statement matches (log_min_duration_statement must be set for statements to be timed).", "Aucune requête chronométrée ne correspond (log_min_duration_statement doit être réglé pour que les requêtes soient chronométrées)."},
}
