UPDATE volunteers
SET shirt = CASE upper(btrim(shirt))
    WHEN 'S' THEN 'MS'
    WHEN 'M' THEN 'MM'
    WHEN 'L' THEN 'ML'
    WHEN 'XL' THEN 'MXL'
    WHEN 'XXL' THEN 'MXXL'
    WHEN 'LS' THEN 'LS'
    WHEN 'LM' THEN 'LM'
    WHEN 'LL' THEN 'LL'
    WHEN 'MS' THEN 'MS'
    WHEN 'MM' THEN 'MM'
    WHEN 'ML' THEN 'ML'
    WHEN 'MXL' THEN 'MXL'
    WHEN 'MXXL' THEN 'MXXL'
    WHEN 'MXXXL' THEN 'MXXXL'
    ELSE ''
END;
