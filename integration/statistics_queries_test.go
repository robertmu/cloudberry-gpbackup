package integration

import (
	"sort"

	"github.com/apache/cloudberry-backup/backup"
	"github.com/apache/cloudberry-backup/testutils"
	"github.com/apache/cloudberry-go-libs/structmatcher"
	"github.com/apache/cloudberry-go-libs/testhelper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup integration tests", func() {
	tables := []backup.Table{
		{Relation: backup.Relation{Schema: "public", Name: "foo"}},
	}
	var tableOid uint32
	BeforeEach(func() {
		testhelper.AssertQueryRuns(connectionPool, "CREATE TABLE public.foo(i int, j text, k bool)")
		tableOid = testutils.OidFromObjectName(connectionPool, "public", "foo", backup.TYPE_RELATION)
		testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.foo VALUES (1, 'a', 't')")
		testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.foo VALUES (2, 'b', 'f')")
		testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.foo VALUES (3, 'c', 't')")
		testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.foo VALUES (4, 'd', 'f')")
		testhelper.AssertQueryRuns(connectionPool, "ANALYZE public.foo")
	})
	AfterEach(func() {
		testhelper.AssertQueryRuns(connectionPool, "DROP TABLE public.foo")
	})
	Describe("GetAttributeStatistics", func() {
		It("returns attribute statistics for a table", func() {
			attStats := backup.GetAttributeStatistics(connectionPool, tables)
			Expect(attStats).To(HaveLen(1))
			Expect(attStats[tableOid]).To(HaveLen(3))
			tableAttStatsI := attStats[tableOid][0]
			tableAttStatsJ := attStats[tableOid][1]
			tableAttStatsK := attStats[tableOid][2]

			/*
			 * Attribute statistics will vary by GPDB version, but statistics for a
			 * certain table should always be the same in a particular version given
			 * the same schema and data.
			 */
			expectedStats5I := backup.AttributeStatistic{Oid: tableOid, Schema: "public", Table: "foo", AttName: "i",
				Type: "int4", Relid: tableOid, AttNumber: 1, Inherit: false, Width: 4, Distinct: -1, Kind1: 2, Kind2: 3, Operator1: 97,
				Operator2: 97, Numbers2: []string{"1"}, Values1: []string{"1", "2", "3", "4"}}
			expectedStats5J := backup.AttributeStatistic{Oid: tableOid, Schema: "public", Table: "foo", AttName: "j",
				Type: "text", Relid: tableOid, AttNumber: 2, Inherit: false, Width: 2, Distinct: -1, Kind1: 2, Kind2: 3, Operator1: 664,
				Operator2: 664, Numbers2: []string{"1"}, Values1: []string{"a", "b", "c", "d"}}
			expectedStats5K := backup.AttributeStatistic{Oid: tableOid, Schema: "public", Table: "foo", AttName: "k",
				Type: "bool", Relid: tableOid, AttNumber: 3, Inherit: false, Width: 1, Distinct: -0.5, Kind1: 1, Kind2: 3, Operator1: 91,
				Operator2: 58, Numbers1: []string{"0.5", "0.5"}, Numbers2: []string{"0.5"}, Values1: []string{"f", "t"}}
			if (connectionPool.Version.IsGPDB() && connectionPool.Version.AtLeast("7")) || connectionPool.Version.IsCBDB() {
				expectedStats5J.Collation1 = 100
				expectedStats5J.Collation2 = 100
			}
			if connectionPool.Version.IsCBDB() && connectionPool.Version.AtLeast("2.1.0") {
				// Cloudberry Database 2.1.0 introduced STATISTIC_KIND_NDV_BY_SEGMENTS (8).
				// In this test case, due to the small data volume, this statistic is
				// automatically placed into the 3rd slot (stakind3) by the analyze command.
				expectedStats5I.Kind3 = backup.STATISTIC_KIND_NDV_BY_SEGMENTS
				expectedStats5J.Kind3 = backup.STATISTIC_KIND_NDV_BY_SEGMENTS
				expectedStats5K.Kind3 = backup.STATISTIC_KIND_NDV_BY_SEGMENTS

				// Set the operator OID for this new statistic kind
				// i (int4) uses operator 97 (=)
				// j (text) uses operator 664 (=) and collation 100
				// k (bool) uses operator 58 (=)
				expectedStats5I.Operator3 = 97
				expectedStats5J.Operator3 = 664
				expectedStats5J.Collation3 = 100
				expectedStats5K.Operator3 = 58

				// 4 distinct rows were inserted for 'i' (int) and 'j' (text) columns
				expectedStats5I.Values3 = []string{"4"}
				expectedStats5J.Values3 = []string{"4"}

				// Why is 'k' (bool) 3.0000000596046448 instead of 2?
				// 1. STATISTIC_KIND_NDV_BY_SEGMENTS (8) is the SUM of local NDVs across all segments, NOT the global NDV.
				// 2. Based on the hash distribution (using 'i' as distribution key), the rows map to segments like so:
				//    - Seg 0 gets 3 rows: (2,b,f), (3,c,t), (4,d,f). Local NDV for 'k' on Seg 0 = 2 ('f' and 't')
				//    - Seg 1 gets 1 row: (1,a,t). Local NDV for 'k' on Seg 1 = 1 ('t')
				//    - Seg 2 gets 0 rows. Local NDV for 'k' on Seg 2 = 0
				//    - Sum of Local NDVs = 2 + 1 + 0 = 3
				// 3. The optimizer uses this to estimate intermediate rows generated during a two-stage aggregation (Partial Agg).
				// 4. The value is stored internally as a float4 (single precision) to save space, and when retrieved,
				//    it is converted back to double precision (float8), resulting in the slight precision loss (3.0000000596046448).
				expectedStats5K.Values3 = []string{"3.0000000596046448"}
			}

			// The order in which the stavalues1 values is returned is not guaranteed to be deterministic
			sort.Strings(tableAttStatsI.Values1)
			sort.Strings(tableAttStatsJ.Values1)
			sort.Strings(tableAttStatsK.Values1)
			structmatcher.ExpectStructsToMatchExcluding(&expectedStats5I, &tableAttStatsI, "Numbers2")
			structmatcher.ExpectStructsToMatchExcluding(&expectedStats5J, &tableAttStatsJ, "Numbers2")
			structmatcher.ExpectStructsToMatchExcluding(&expectedStats5K, &tableAttStatsK, "Numbers2")

		})
	})
	Describe("GetTupleStatistics", func() {
		It("returns tuple statistics for a table", func() {
			tupleStats := backup.GetTupleStatistics(connectionPool, tables)
			Expect(tupleStats).To(HaveLen(1))
			tableTupleStats := tupleStats[tableOid]

			// Tuple statistics will not vary by GPDB version. Relpages may vary based on the hardware.
			expectedStats := backup.TupleStatistic{Oid: tableOid, Schema: "public", Table: "foo", RelTuples: 4}

			structmatcher.ExpectStructsToMatchExcluding(&expectedStats, &tableTupleStats, "RelPages")
		})
	})
})
