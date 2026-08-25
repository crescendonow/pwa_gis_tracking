"""แปลงช่วงเวลาเป็น datetime object — MongoDB เก็บวันที่เป็น BSON date"""

from datetime import datetime


def year_range(ce_year):
    """ปี ค.ศ. → (start, end) เป็น datetime"""
    return datetime(ce_year, 1, 1), datetime(ce_year + 1, 1, 1)


def month_range(ce_year, month):
    start = datetime(ce_year, month, 1)
    end = datetime(ce_year + 1, 1, 1) if month == 12 else datetime(ce_year, month + 1, 1)
    return start, end


def fiscal_range(be_year):
    """ปีงบประมาณ พ.ศ. → 1 ต.ค. (ปีก่อน) ถึง 1 ต.ค. (ปีนั้น)"""
    ce = be_year - 543 if be_year > 2400 else be_year
    return datetime(ce - 1, 10, 1), datetime(ce, 10, 1)
