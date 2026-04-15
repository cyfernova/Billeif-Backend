ALTER TABLE invoice_items
    ADD COLUMN IF NOT EXISTS unit VARCHAR(20) NOT NULL DEFAULT 'OTH',
    ADD COLUMN IF NOT EXISTS hsn_sac_code VARCHAR(40) NOT NULL DEFAULT '';

DO $$
DECLARE
    has_ii_uqc  BOOLEAN;
    has_p_unit  BOOLEAN;
    has_p_uqc   BOOLEAN;
    has_p_hsn   BOOLEAN;
    unit_source TEXT := 'COALESCE(NULLIF(ii.unit, '''')';
    hsn_source  TEXT := 'COALESCE(NULLIF(ii.hsn_sac_code, '''')';
    backfill_sql TEXT;
BEGIN
    SELECT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'invoice_items' AND column_name = 'uqc_code'
    ) INTO has_ii_uqc;

    SELECT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'products' AND column_name = 'unit'
    ) INTO has_p_unit;

    SELECT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'products' AND column_name = 'uqc_code'
    ) INTO has_p_uqc;

    SELECT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'products' AND column_name = 'hsn_sac_code'
    ) INTO has_p_hsn;

    IF has_ii_uqc THEN
        unit_source := unit_source || ', NULLIF(ii.uqc_code, '''')';
    END IF;

    IF has_p_unit THEN
        unit_source := unit_source || ', NULLIF(p.unit, '''')';
    END IF;

    IF has_p_uqc THEN
        unit_source := unit_source || ', NULLIF(p.uqc_code, '''')';
    END IF;

    unit_source := unit_source || ', '''')';

    IF has_p_hsn THEN
        hsn_source := hsn_source || ', NULLIF(p.hsn_sac_code, '''')';
    END IF;

    hsn_source := hsn_source || ', '''')';

    backfill_sql := format(
        'UPDATE invoice_items ii
         SET unit = CASE
                WHEN UPPER(TRIM(%1$s)) IN (
                    ''BAG'',''BAL'',''BDL'',''BKL'',''BOU'',''BOX'',''BTL'',''BUN'',''CAN'',''CBM'',''CCM'',''CMS'',''CTN'',''DOZ'',''DRM'',''GGK'',''GMS'',''GRS'',
                    ''GYD'',''KGS'',''KLR'',''KME'',''LTR'',''MLT'',''MTR'',''MTS'',''NOS'',''PAC'',''PCS'',''PRS'',''QTL'',''ROL'',''SET'',''SQF'',''SQM'',''SQY'',
                    ''TBS'',''TGM'',''THD'',''TON'',''TUB'',''UGS'',''UNT'',''YDS'',''OTH''
                ) THEN UPPER(TRIM(%1$s))
                WHEN %1$s = '''' THEN ''OTH''
                ELSE ''OTH''
             END,
             hsn_sac_code = %2$s
         FROM products p
         WHERE ii.product_id = p.id;',
        unit_source,
        hsn_source
    );

    EXECUTE backfill_sql;
END $$;

UPDATE invoice_items
SET unit = CASE
        WHEN UPPER(TRIM(COALESCE(NULLIF(unit, ''), ''))) IN (
            'BAG','BAL','BDL','BKL','BOU','BOX','BTL','BUN','CAN','CBM','CCM','CMS','CTN','DOZ','DRM','GGK','GMS','GRS',
            'GYD','KGS','KLR','KME','LTR','MLT','MTR','MTS','NOS','PAC','PCS','PRS','QTL','ROL','SET','SQF','SQM','SQY',
            'TBS','TGM','THD','TON','TUB','UGS','UNT','YDS','OTH'
        ) THEN UPPER(TRIM(unit))
        WHEN COALESCE(NULLIF(unit, ''), '') = '' THEN 'OTH'
        ELSE 'OTH'
    END,
    hsn_sac_code = COALESCE(hsn_sac_code, '')
;
